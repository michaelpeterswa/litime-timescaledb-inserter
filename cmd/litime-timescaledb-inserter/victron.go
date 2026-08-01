package main

import (
	"context"
	"log/slog"
	"time"

	victronble "alpineworks.io/go-victron-ble"
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
//
// Record types go to different tables, so this dispatches on which payload the
// collector filled in rather than on RecordType: the pointer is what the rest
// of the function actually needs, and checking it removes any way for the two
// to disagree.
func insertVictron(
	ctx context.Context,
	client *timescale.TimescaleClient,
	metrics *victron.Metrics,
	timeout time.Duration,
	reading victron.Reading,
) {
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	switch {
	case reading.SolarCharger != nil:
		insertSolarCharger(insertCtx, client, metrics, reading)
	case reading.BatteryMonitor != nil:
		insertBatteryMonitor(insertCtx, client, metrics, reading)
	default:
		// The collector does not publish a reading without a payload, so this
		// is a programming error rather than a device fault.
		slog.Error("victron reading carried no payload",
			slog.String("device_id", reading.DeviceID),
			slog.Int("record_type", int(reading.RecordType)))
	}
}

func insertSolarCharger(
	ctx context.Context,
	client *timescale.TimescaleClient,
	metrics *victron.Metrics,
	reading victron.Reading,
) {
	charger := reading.SolarCharger

	measure := timescale.VictronMeasurement{
		DeviceID:               reading.DeviceID,
		ObservedAt:             reading.ObservedAt,
		ModelID:                reading.ModelID,
		ModelName:              reading.ModelName,
		RecordType:             reading.RecordType,
		BatteryVoltage:         charger.BatteryVoltage,
		BatteryChargingCurrent: charger.BatteryChargingCurrent,
		YieldToday:             charger.YieldToday,
		SolarPower:             charger.SolarPower,
		ExternalDeviceLoad:     charger.ExternalDeviceLoad,
	}

	// The enums are stored as text so the table reads without a lookup, and
	// stay NULL when the device reported the field as unavailable.
	if charger.ChargeState != nil {
		state := charger.ChargeState.String()
		measure.ChargeState = &state
	}
	if charger.ChargerError != nil {
		chargerErr := charger.ChargerError.String()
		measure.ChargerError = &chargerErr
	}

	err := client.InsertVictron(ctx, measure)
	metrics.RecordInsert(ctx, reading.DeviceID, err)

	if err != nil {
		slog.Error("failed to insert victron data into timescale",
			slog.String("device_id", reading.DeviceID),
			slog.String("error", err.Error()))
		return
	}

	slog.Debug("inserted victron reading",
		slog.String("device_id", reading.DeviceID),
		slog.Any("solar_power", charger.SolarPower),
		slog.Any("battery_voltage", charger.BatteryVoltage))
}

func insertBatteryMonitor(
	ctx context.Context,
	client *timescale.TimescaleClient,
	metrics *victron.Metrics,
	reading victron.Reading,
) {
	monitor := reading.BatteryMonitor

	measure := timescale.VictronBatteryMonitorMeasurement{
		DeviceID:       reading.DeviceID,
		ObservedAt:     reading.ObservedAt,
		ModelID:        reading.ModelID,
		ModelName:      reading.ModelName,
		RecordType:     reading.RecordType,
		BatteryVoltage: monitor.BatteryVoltage,
		BatteryCurrent: monitor.BatteryCurrent,
		StateOfCharge:  monitor.StateOfCharge,
		ConsumedAh:     monitor.ConsumedAh,
		AuxInputType:   monitor.AuxInputType.String(),
		AuxValue:       auxValue(monitor),
	}

	// Stored in minutes because that is the resolution the device sends; a
	// finer unit would imply precision the reading does not have.
	if monitor.TimeToGo != nil {
		minutes := int32(monitor.TimeToGo.Minutes())
		measure.TimeToGoMinutes = &minutes
	}

	// NULL rather than "none" when nothing is wrong, so that finding trouble is
	// a NULL check rather than a string comparison.
	if monitor.AlarmReason.Active() {
		alarm := monitor.AlarmReason.String()
		measure.AlarmReason = &alarm
	}

	err := client.InsertVictronBatteryMonitor(ctx, measure)
	metrics.RecordInsert(ctx, reading.DeviceID, err)

	if err != nil {
		slog.Error("failed to insert victron battery monitor data into timescale",
			slog.String("device_id", reading.DeviceID),
			slog.String("error", err.Error()))
		return
	}

	slog.Debug("inserted victron battery monitor reading",
		slog.String("device_id", reading.DeviceID),
		slog.Any("battery_voltage", monitor.BatteryVoltage),
		slog.Any("battery_current", monitor.BatteryCurrent),
		slog.Any("state_of_charge", monitor.StateOfCharge))
}

// auxValue picks whichever aux reading the device populated.
//
// The library exposes the three as separate typed fields precisely so volts and
// degrees cannot be confused; flattening them into one column here is safe only
// because aux_input_type is stored alongside and says which this is.
func auxValue(monitor *victronble.BatteryMonitor) *float64 {
	switch {
	case monitor.StarterVoltage != nil:
		return monitor.StarterVoltage
	case monitor.MidpointVoltage != nil:
		return monitor.MidpointVoltage
	case monitor.Temperature != nil:
		return monitor.Temperature
	default:
		return nil
	}
}
