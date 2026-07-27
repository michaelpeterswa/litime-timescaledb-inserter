package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"alpineworks.io/ootel"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/config"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/litime"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/logging"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/timescale"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
)

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}

	slogLevel, err := logging.LogLevelToSlogLevel(logLevel)
	if err != nil {
		log.Fatalf("could not convert log level: %s", err)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	})))
	c, err := config.NewConfig()
	if err != nil {
		slog.Error("could not create config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Cancelled on SIGINT/SIGTERM, which unwinds the battery supervisors and
	// closes the reading queue so the writer below can drain and exit.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	exporterType := ootel.ExporterTypePrometheus
	if c.Local {
		exporterType = ootel.ExporterTypeOTLPGRPC
	}

	ootelClient := ootel.NewOotelClient(
		ootel.WithMetricConfig(
			ootel.NewMetricConfig(
				c.MetricsEnabled,
				exporterType,
				c.MetricsPort,
			),
		),
		ootel.WithTraceConfig(
			ootel.NewTraceConfig(
				c.TracingEnabled,
				c.TracingSampleRate,
				c.TracingService,
				c.TracingVersion,
			),
		),
	)

	shutdown, err := ootelClient.Init(ctx)
	if err != nil {
		slog.Error("could not create ootel client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(5 * time.Second))
	if err != nil {
		slog.Error("could not create runtime metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	err = host.Start()
	if err != nil {
		slog.Error("could not create host metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	defer func() {
		_ = shutdown(context.Background())
	}()

	batteries, err := litime.ParseBatteries(c.LitimeBatteries, c.LitimeBatteryBluetoothName)
	if err != nil {
		slog.Error("could not determine batteries to poll", slog.String("error", err.Error()))
		os.Exit(1)
	}

	for _, battery := range batteries {
		// Log how each battery will be located: a mistyped address falls back to
		// a name lookup, and this is where that becomes visible.
		slog.Info("battery configured",
			slog.String("battery_id", battery.ID),
			slog.String("match", string(battery.Match)),
			slog.String("value", battery.Value))
	}

	timescaleClient, err := timescale.NewTimescaleClient(ctx, c.TimescaleConnString)
	if err != nil {
		slog.Error("could not create timescale client", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer timescaleClient.Close()

	metrics, err := litime.NewMetrics()
	if err != nil {
		slog.Error("could not create metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	manager, err := litime.NewManager(litime.ManagerOptions{
		Batteries:      batteries,
		ScrapeInterval: c.ScrapeInterval,
		ScanTimeout:    c.ScanTimeout,
		StaleTimeout:   c.StaleTimeout,
		BufferSize:     c.ReadingBufferSize,
		Logger:         slog.Default(),
		Metrics:        metrics,
	})
	if err != nil {
		slog.Error("could not create battery manager", slog.String("error", err.Error()))
		os.Exit(1)
	}

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- manager.Run(ctx)
	}()

	slog.Info("litime timescaledb inserter started",
		slog.String("scrape_interval", c.ScrapeInterval.String()),
		slog.String("stale_timeout", c.StaleTimeout.String()),
		slog.Int("batteries", len(batteries)))

	// Writing happens here rather than in the Bluetooth callbacks so that a slow
	// database cannot stall notification dispatch. The loop ends when the
	// manager closes the queue, after draining whatever is still in it.
	for reading := range manager.Readings() {
		insert(ctx, timescaleClient, metrics, c.CallbackTimeout, reading)
	}

	if err := <-managerDone; err != nil {
		slog.Error("battery manager stopped with an error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}

// insert writes one reading, using a background context so that readings still
// in the queue at shutdown are not abandoned mid-write.
func insert(ctx context.Context, client *timescale.TimescaleClient, metrics *litime.Metrics, timeout time.Duration, reading litime.Reading) {
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	err := client.Insert(insertCtx, reading.BatteryID, reading.ObservedAt, reading.Data)
	metrics.RecordInsert(insertCtx, reading.BatteryID, err)

	if err != nil {
		slog.Error("failed to insert data into timescale",
			slog.String("battery_id", reading.BatteryID),
			slog.String("error", err.Error()))
		return
	}

	slog.Debug("inserted reading",
		slog.String("battery_id", reading.BatteryID),
		slog.Int("soc", int(reading.Data.SOC)),
		slog.Float64("total_voltage", float64(reading.Data.TotalVoltage)))
}
