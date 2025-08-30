-- Remove the announce column by recreating the table without it
BEGIN TRANSACTION;

-- Create a new table without the announce column
CREATE TABLE guild_settings__old (
    guild_id   TEXT PRIMARY KEY,
    channel_id TEXT,
    timezone   TEXT,
    enabled    INTEGER,
    org        TEXT,
    run_hour   INTEGER
);

-- Copy existing data sans announce
INSERT INTO guild_settings__old (guild_id, channel_id, timezone, enabled, org, run_hour)
SELECT guild_id, channel_id, timezone, enabled, org, run_hour
FROM guild_settings;

-- Replace the original table
DROP TABLE guild_settings;
ALTER TABLE guild_settings__old RENAME TO guild_settings;

COMMIT;

