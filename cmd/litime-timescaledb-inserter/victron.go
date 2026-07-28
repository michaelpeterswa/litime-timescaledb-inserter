package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/config"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/timescale"
	"github.com/michaelpeterswa/litime-timescaledb-inserter/internal/victron"
	tinygobluetooth "tinygo.org/x/bluetooth"
)

// startVictron brings up Victron collection if any devices are configured,
// returning a channel that reports how it finished and the number of devices.
//
// Victron support is optional: with nothing configured the returned channel
// yields nil immediately, so the caller can wait on it unconditionally.
func startVictron(
	ctx context.Context,
	c *config.Config,
	adapter *tinygobluetooth.Adapter,
	timescaleClient *timescale.TimescaleClient,
) (<-chan error, int, error) {
	done := make(chan error, 1)

	devices, err := victron.ParseDevices(c.VictronDevices, c.VictronKeys)
	if err != nil {
		return nil, 0, err
	}

	if len(devices) == 0 {
		done <- nil
		close(done)
		return done, 0, nil
	}

	metrics, err := victron.NewMetrics()
	if err != nil {
		return nil, 0, err
	}

	collector, err := victron.NewCollector(victron.CollectorOptions{
		Devices:      devices,
		ScanInterval: c.VictronScanInterval,
		ScanDuration: c.VictronScanDuration,
		BufferSize:   c.ReadingBufferSize,
		Adapter:      adapter,
		Logger:       slog.Default(),
		Metrics:      metrics,
	})
	if err != nil {
		return nil, 0, err
	}

	for _, device := range devices {
		// The key is deliberately not logged.
		slog.Info("victron device configured",
			slog.String("device_id", device.ID),
			slog.String("address", device.Address))
	}

	collectorDone := make(chan error, 1)
	go func() {
		collectorDone <- collector.Run(ctx)
	}()

	go func() {
		// Draining to completion matters: the collector closes the queue when
		// it stops, and readings still in it are written before we report done.
		for reading := range collector.Readings() {
			insertVictron(ctx, timescaleClient, metrics, c.CallbackTimeout, reading)
		}
		done <- <-collectorDone
		close(done)
	}()

	return done, len(devices), nil
}

// insertVictron writes one reading, using a background context so that readings
// still queued at shutdown are not abandoned mid-write.
func insertVictron(
	ctx context.Context,
	client *timescale.TimescaleClient,
	metrics *victron.Metrics,
	timeout time.Duration,
	reading victron.Reading,
) {
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	measure := timescale.VictronMeasurement{
		DeviceID:               reading.DeviceID,
		ObservedAt:             reading.ObservedAt,
		ModelID:                reading.ModelID,
		ModelName:              reading.ModelName,
		RecordType:             reading.RecordType,
		BatteryVoltage:         reading.Data.BatteryVoltage,
		BatteryChargingCurrent: reading.Data.BatteryChargingCurrent,
		YieldToday:             reading.Data.YieldToday,
		SolarPower:             reading.Data.SolarPower,
		ExternalDeviceLoad:     reading.Data.ExternalDeviceLoad,
	}

	// The enums are stored as text so the table reads without a lookup, and
	// stay NULL when the device reported the field as unavailable.
	if reading.Data.ChargeState != nil {
		state := reading.Data.ChargeState.String()
		measure.ChargeState = &state
	}
	if reading.Data.ChargerError != nil {
		chargerErr := reading.Data.ChargerError.String()
		measure.ChargerError = &chargerErr
	}

	err := client.InsertVictron(insertCtx, measure)
	metrics.RecordInsert(insertCtx, reading.DeviceID, err)

	if err != nil {
		slog.Error("failed to insert victron data into timescale",
			slog.String("device_id", reading.DeviceID),
			slog.String("error", err.Error()))
		return
	}

	slog.Debug("inserted victron reading",
		slog.String("device_id", reading.DeviceID),
		slog.Any("solar_power", reading.Data.SolarPower),
		slog.Any("battery_voltage", reading.Data.BatteryVoltage))
}
