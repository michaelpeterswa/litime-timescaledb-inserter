-- Identify which battery a measurement came from. Without this, readings from
-- several batteries are indistinguishable once they are in the table.
--
-- Existing rows predate multi-battery support and so came from a single unnamed
-- battery; they are backfilled as 'unknown' rather than guessed at. The default
-- is dropped afterwards so a future insert that forgets the column fails loudly
-- instead of quietly landing in the 'unknown' bucket.
ALTER TABLE sensors.litime
ADD COLUMN IF NOT EXISTS battery_id TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE sensors.litime
ALTER COLUMN battery_id
DROP DEFAULT;

-- Almost every query is "this battery, recent first".
CREATE INDEX IF NOT EXISTS litime_battery_id_time_idx ON sensors.litime (battery_id, time DESC);
