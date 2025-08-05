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

func (c *TimescaleClient) Insert(ctx context.Context, measure *golitimebluetooth.LiTimeBatteryData) error {
	_, err := c.Pool.Exec(ctx, insertLitime,
		time.Now(),
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

func mapCellVoltages(cellVoltages []float32) map[string]float32 {
	voltages := make(map[string]float32, len(cellVoltages))
	for i, voltage := range cellVoltages {
		voltages[fmt.Sprintf("cell_%d", i)] = voltage
	}
	return voltages
}
