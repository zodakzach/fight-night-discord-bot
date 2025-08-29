CREATE TABLE IF NOT EXISTS guild_settings (
    guild_id   TEXT PRIMARY KEY,
    channel_id TEXT,
    timezone   TEXT,
    enabled    INTEGER,
    org        TEXT
);

CREATE TABLE IF NOT EXISTS last_posted (
    guild_id  TEXT NOT NULL,
    sport     TEXT NOT NULL,
    last_date TEXT NOT NULL,
    PRIMARY KEY (guild_id, sport)
);

