CREATE TABLE IF NOT EXISTS
    sensors.litime (
        time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        total_voltage REAL,
        cell_voltage_sum REAL,
        current REAL,
        mosfet_temp SMALLINT,
        cell_temp SMALLINT,
        remaining_ah REAL,
        full_capacity_ah REAL,
        protection_state TEXT,
        heat_state TEXT,
        balance_memory TEXT,
        failure_state TEXT,
        balancing_state TEXT,
        battery_state TEXT,
        soc SMALLINT,
        soh TEXT,
        discharges_count INTEGER,
        discharges_ah_count REAL,
        cell_voltages JSONB
    );

SELECT
    create_hypertable ('sensors.litime', 'time', if_not_exists => TRUE);