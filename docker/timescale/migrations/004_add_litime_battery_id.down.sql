DROP INDEX IF EXISTS sensors.litime_battery_id_time_idx;

ALTER TABLE sensors.litime
DROP COLUMN IF EXISTS battery_id;
