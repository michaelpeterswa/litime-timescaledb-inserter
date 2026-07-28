-- Readings from Victron devices broadcasting Instant Readout over BLE.
--
-- device_id is operator-chosen and is the only thing tying a row to a physical
-- unit, so it is required rather than defaulted. model and record_type come
-- from the advertisement itself and are recorded so a replaced or reconfigured
-- device is visible in the data rather than silently changing meaning.
--
-- Every reading is nullable because Victron encodes "not available" as a
-- per-field sentinel. Storing those as 0 would be indistinguishable from a
-- charger genuinely reporting no output.
CREATE TABLE IF NOT EXISTS
    sensors.victron (
        time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        device_id TEXT NOT NULL,
        model_id INTEGER,
        model_name TEXT,
        record_type SMALLINT,
        charge_state TEXT,
        charger_error TEXT,
        battery_voltage REAL,
        battery_charging_current REAL,
        yield_today REAL,
        solar_power REAL,
        external_device_load REAL
    );

SELECT
    create_hypertable ('sensors.victron', 'time', if_not_exists => TRUE);

-- Almost every query is "this device, recent first".
CREATE INDEX IF NOT EXISTS victron_device_id_time_idx ON sensors.victron (device_id, time DESC);

-- The grafana role reads these tables, and a grant per table is easy to forget:
-- sensors.victron was created without one, so the dashboard queried a table it
-- could not see. Grant it, and set the default so later tables are covered
-- without anyone having to remember.
GRANT
SELECT
    ON sensors.victron TO grafana;

ALTER DEFAULT PRIVILEGES IN SCHEMA sensors
GRANT
SELECT
    ON TABLES TO grafana;
