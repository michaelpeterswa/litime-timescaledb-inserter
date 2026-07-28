package timescale

import (
	"context"
	"fmt"
	"time"

	_ "embed"

	golitimebluetooth "alpineworks.io/go-litime-bluetooth"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrorSensorIssue = fmt.Errorf("sensor issue - undefined values")
)

type TimescaleClient struct {
	Pool *pgxpool.Pool
}

func NewTimescaleClient(ctx context.Context, connString string) (*TimescaleClient, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	err = pool.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &TimescaleClient{Pool: pool}, nil
}

func (c *TimescaleClient) Close() {
	c.Pool.Close()
}

//go:embed queries/insert_litime.pgsql
var insertLitime string

// Insert records one measurement against the battery it came from. Callers pass
// the observation time explicitly so a reading is stored with the moment the
// battery reported it rather than the moment it reached the database.
func (c *TimescaleClient) Insert(ctx context.Context, batteryID string, observedAt time.Time, measure *golitimebluetooth.LiTimeBatteryData) error {
	_, err := c.Pool.Exec(ctx, insertLitime,
		observedAt,
		batteryID,
		measure.TotalVoltage,
		measure.CellVoltageSum,
		measure.Current,
		measure.MosfetTemp,
		measure.CellTemp,
		measure.RemainingAh,
		measure.FullCapacityAh,
		measure.ProtectionState,
		measure.HeatState,
		measure.BalanceMemory,
		measure.FailureState,
		measure.BalancingState,
		measure.BatteryState,
		measure.SOC,
		measure.SOH,
		measure.DischargesCount,
		measure.DischargesAhCount,
		mapCellVoltages(measure.CellVoltages),
	)
	if err != nil {
		return fmt.Errorf("insert data: %w", err)
	}

	return nil
}

//go:embed queries/insert_victron.pgsql
var insertVictron string

// VictronMeasurement is one decoded Victron solar charger advertisement.
//
// The readings are pointers because Victron encodes "not available" per field.
// They are stored as NULL rather than zero, since a charger reporting nothing
// and a charger reporting no output are different states.
type VictronMeasurement struct {
	DeviceID   string
	ObservedAt time.Time
	ModelID    uint16
	ModelName  string
	RecordType uint8

	ChargeState            *string
	ChargerError           *string
	BatteryVoltage         *float64
	BatteryChargingCurrent *float64
	YieldToday             *float64
	SolarPower             *float64
	ExternalDeviceLoad     *float64
}

// InsertVictron records one Victron reading.
func (c *TimescaleClient) InsertVictron(ctx context.Context, measure VictronMeasurement) error {
	_, err := c.Pool.Exec(ctx, insertVictron,
		measure.ObservedAt,
		measure.DeviceID,
		int32(measure.ModelID),
		measure.ModelName,
		int16(measure.RecordType),
		measure.ChargeState,
		measure.ChargerError,
		measure.BatteryVoltage,
		measure.BatteryChargingCurrent,
		measure.YieldToday,
		measure.SolarPower,
		measure.ExternalDeviceLoad,
	)
	if err != nil {
		return fmt.Errorf("insert victron data: %w", err)
	}

	return nil
}

func mapCellVoltages(cellVoltages []float32) map[string]float32 {
	voltages := make(map[string]float32, len(cellVoltages))
	for i, voltage := range cellVoltages {
		voltages[fmt.Sprintf("cell_%d", i)] = voltage
	}
	return voltages
}
