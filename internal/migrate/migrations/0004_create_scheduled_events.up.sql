-- Create table to track scheduled event IDs per guild/org/date
CREATE TABLE IF NOT EXISTS scheduled_events (
    guild_id   TEXT NOT NULL,
    sport      TEXT NOT NULL,
    event_date TEXT NOT NULL,
    event_id   TEXT NOT NULL,
    PRIMARY KEY (guild_id, sport, event_date)
);

-- Add per-guild events toggle for scheduled events
ALTER TABLE guild_settings ADD COLUMN events INTEGER;
