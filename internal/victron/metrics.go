package victron

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/michaelpeterswa/litime-timescaledb-inserter/victron"

// Metrics reports per-device collection health.
//
// A passive collector has no connection to lose, so the only evidence that a
// device has gone quiet is the absence of readings. That makes the missed
// counter the one worth alerting on.
type Metrics struct {
	readings        metric.Int64Counter
	missed          metric.Int64Counter
	decryptFailures metric.Int64Counter
	decodeFailures  metric.Int64Counter
	scanFailures    metric.Int64Counter
	dropped         metric.Int64Counter
	inserts         metric.Int64Counter
	insertErrors    metric.Int64Counter
}

func NewMetrics() (*Metrics, error) {
	meter := otel.Meter(meterName)

	// counter returns the first error encountered, so only the final check is
	// needed rather than one after every counter.
	var err error
	counter := func(name, description string) metric.Int64Counter {
		c, cErr := meter.Int64Counter(name, metric.WithDescription(description))
		if cErr != nil && err == nil {
			err = cErr
		}
		return c
	}

	m := &Metrics{
		readings:        counter("victron.readings", "Advertisements decoded from a Victron device"),
		missed:          counter("victron.missed", "Scans that produced no advertisement for a configured device"),
		decryptFailures: counter("victron.decrypt_failures", "Advertisements that could not be decrypted"),
		decodeFailures:  counter("victron.decode_failures", "Advertisements that decrypted but could not be parsed"),
		scanFailures:    counter("victron.scan_failures", "Scans that failed outright"),
		dropped:         counter("victron.readings_dropped", "Readings discarded because the write queue was full"),
		inserts:         counter("victron.inserts", "Readings written to the database"),
		insertErrors:    counter("victron.insert_errors", "Readings that failed to be written to the database"),
	}

	if err != nil {
		return nil, err
	}

	return m, nil
}

// The recorders below tolerate a nil receiver so metrics stay optional.

func device(id string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("device_id", id))
}

func (m *Metrics) recordReading(ctx context.Context, deviceID string) {
	if m == nil {
		return
	}
	m.readings.Add(ctx, 1, device(deviceID))
}

func (m *Metrics) recordMissed(ctx context.Context, deviceID string) {
	if m == nil {
		return
	}
	m.missed.Add(ctx, 1, device(deviceID))
}

func (m *Metrics) recordDecryptFailure(ctx context.Context, deviceID string) {
	if m == nil {
		return
	}
	m.decryptFailures.Add(ctx, 1, device(deviceID))
}

func (m *Metrics) recordDecodeFailure(ctx context.Context, deviceID string) {
	if m == nil {
		return
	}
	m.decodeFailures.Add(ctx, 1, device(deviceID))
}

func (m *Metrics) recordScanFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.scanFailures.Add(ctx, 1)
}

func (m *Metrics) recordDropped(ctx context.Context, deviceID string) {
	if m == nil {
		return
	}
	m.dropped.Add(ctx, 1, device(deviceID))
}

// RecordInsert reports the outcome of writing one reading to the database.
func (m *Metrics) RecordInsert(ctx context.Context, deviceID string, err error) {
	if m == nil {
		return
	}

	if err != nil {
		m.insertErrors.Add(ctx, 1, device(deviceID))
		return
	}

	m.inserts.Add(ctx, 1, device(deviceID))
}
