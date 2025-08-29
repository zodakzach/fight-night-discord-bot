-- SQLite does not support DROP COLUMN; recreate table without the added columns.
BEGIN TRANSACTION;

-- Create a new table with the original schema (from 0001)
CREATE TABLE guild_settings__old (
    guild_id   TEXT PRIMARY KEY,
    channel_id TEXT,
    timezone   TEXT,
    enabled    INTEGER,
    org        TEXT
);

-- Copy existing data, ignoring the run_at/run_hour columns
INSERT INTO guild_settings__old (guild_id, channel_id, timezone, enabled, org)
SELECT guild_id, channel_id, timezone, enabled, org
FROM guild_settings;

-- Replace the original table
DROP TABLE guild_settings;
ALTER TABLE guild_settings__old RENAME TO guild_settings;

COMMIT;

