package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	golitimebluetooth "alpineworks.io/go-litime-bluetooth"
	"alpineworks.io/go-litime-bluetooth/bluetooth"
	"alpineworks.io/ootel"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/config"
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

	ctx := context.Background()

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
		_ = shutdown(ctx)
	}()

	timescaleClient, err := timescale.NewTimescaleClient(ctx, c.TimescaleConnString)
	if err != nil {
		slog.Error("could not create timescale client", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer timescaleClient.Close()

	liTimeClient := bluetooth.NewLiTimeBluetoothClient(c.LitimeBatteryBluetoothName, bluetooth.WithLogger(slog.Default()),
		bluetooth.WithEnableNotificationCallback(func(b []byte) {
			callbackCtx, cancel := context.WithTimeout(ctx, c.CallbackTimeout)
			defer cancel()

			data, err := golitimebluetooth.ParseLiTimeBatteryData(b)
			if err != nil {
				slog.Error("failed to parse notification data", slog.String("error", err.Error()))
				return
			}
			slog.Info("received notification data", slog.Any("data", data))

			err = timescaleClient.Insert(callbackCtx, data)
			if err != nil {
				slog.Error("failed to insert data into timescale", slog.String("error", err.Error()))
				return
			}
		}),
	)

	// Connect to the LiTime battery
	err = liTimeClient.Connect(ctx)
	if err != nil {
		slog.Error("could not connect to litime battery", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Start periodic data collection goroutine
	go func() {
		ticker := time.NewTicker(c.ScrapeInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("stopping periodic data collection")
				return
			case <-ticker.C:
				err := liTimeClient.QueryData()
				if err != nil {
					slog.Error("failed to query battery data", slog.String("error", err.Error()))
					continue
				}
			}
		}
	}()

	slog.Info("litime timescaledb inserter started",
		slog.String("scrape_interval", c.ScrapeInterval.String()),
		slog.String("battery_name", c.LitimeBatteryBluetoothName))

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	slog.Info("received signal, shutting down gracefully", slog.String("signal", sig.String()))
}
