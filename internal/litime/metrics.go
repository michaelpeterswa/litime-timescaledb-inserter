package litime

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/michaelpeterswa/litime-timescaledb-inserter"

// Metrics reports per-battery health.
//
// Everything here is labelled by battery, because with several batteries the
// operational question stops being "is it running" and becomes "is one of them
// quietly dead". Aggregate counters cannot answer that.
type Metrics struct {
	connected     metric.Int64Gauge
	notifications metric.Int64Counter
	reconnects    metric.Int64Counter
	dropped       metric.Int64Counter
	parseFailures metric.Int64Counter
	inserts       metric.Int64Counter
	insertErrors  metric.Int64Counter
}

func NewMetrics() (*Metrics, error) {
	meter := otel.Meter(meterName)

	connected, err := meter.Int64Gauge("litime.battery.connected",
		metric.WithDescription("Whether a battery is currently connected (1) or not (0)"))
	if err != nil {
		return nil, err
	}

	notifications, err := meter.Int64Counter("litime.battery.notifications",
		metric.WithDescription("Readings received from a battery"))
	if err != nil {
		return nil, err
	}

	reconnects, err := meter.Int64Counter("litime.battery.reconnects",
		metric.WithDescription("Times a battery connection was re-established"))
	if err != nil {
		return nil, err
	}

	dropped, err := meter.Int64Counter("litime.battery.readings_dropped",
		metric.WithDescription("Readings discarded because the write queue was full"))
	if err != nil {
		return nil, err
	}

	parseFailures, err := meter.Int64Counter("litime.battery.parse_failures",
		metric.WithDescription("Notifications that could not be parsed"))
	if err != nil {
		return nil, err
	}

	inserts, err := meter.Int64Counter("litime.inserts",
		metric.WithDescription("Readings written to the database"))
	if err != nil {
		return nil, err
	}

	insertErrors, err := meter.Int64Counter("litime.insert_errors",
		metric.WithDescription("Readings that failed to be written to the database"))
	if err != nil {
		return nil, err
	}

	return &Metrics{
		connected:     connected,
		notifications: notifications,
		reconnects:    reconnects,
		dropped:       dropped,
		parseFailures: parseFailures,
		inserts:       inserts,
		insertErrors:  insertErrors,
	}, nil
}

// The recorders below tolerate a nil receiver so that metrics stay optional and
// callers are not littered with nil checks.

func battery(id string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("battery_id", id))
}

func (m *Metrics) recordConnected(ctx context.Context, batteryID string, connected bool) {
	if m == nil {
		return
	}

	var value int64
	if connected {
		value = 1
	}

	m.connected.Record(ctx, value, battery(batteryID))
}

func (m *Metrics) recordNotification(ctx context.Context, batteryID string) {
	if m == nil {
		return
	}

	m.notifications.Add(ctx, 1, battery(batteryID))
}

func (m *Metrics) recordReconnect(ctx context.Context, batteryID string) {
	if m == nil {
		return
	}

	m.reconnects.Add(ctx, 1, battery(batteryID))
}

func (m *Metrics) recordDropped(ctx context.Context, batteryID string) {
	if m == nil {
		return
	}

	m.dropped.Add(ctx, 1, battery(batteryID))
}

func (m *Metrics) recordParseFailure(ctx context.Context, batteryID string) {
	if m == nil {
		return
	}

	m.parseFailures.Add(ctx, 1, battery(batteryID))
}

// RecordInsert reports the outcome of writing one reading to the database.
func (m *Metrics) RecordInsert(ctx context.Context, batteryID string, err error) {
	if m == nil {
		return
	}

	if err != nil {
		m.insertErrors.Add(ctx, 1, battery(batteryID))
		return
	}

	m.inserts.Add(ctx, 1, battery(batteryID))
}
