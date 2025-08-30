-- Drop the scheduled_events table and remove the events column by recreating guild_settings
DROP TABLE IF EXISTS scheduled_events;

BEGIN TRANSACTION;

-- Recreate guild_settings without the events column (keep prior columns)
CREATE TABLE guild_settings__old (
    guild_id   TEXT PRIMARY KEY,
    channel_id TEXT,
    timezone   TEXT,
    enabled    INTEGER,
    org        TEXT,
    run_hour   INTEGER,
    announce   INTEGER
);

INSERT INTO guild_settings__old (guild_id, channel_id, timezone, enabled, org, run_hour, announce)
SELECT guild_id, channel_id, timezone, enabled, org, run_hour, announce
FROM guild_settings;

DROP TABLE guild_settings;
ALTER TABLE guild_settings__old RENAME TO guild_settings;

COMMIT;
