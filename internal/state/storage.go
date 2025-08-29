package state

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
)

// Store provides persistent guild configuration and last-posted state
// backed by a SQLite database via sqlx.
type Store struct {
	db *sqlx.DB
}

// GuildConfig mirrors persisted guild settings for convenience where needed.
type GuildConfig struct {
	ChannelID  string
	Timezone   string
	LastPosted map[string]string // sport -> YYYY-MM-DD
}

// Load opens (or creates) a SQLite DB at the given path and ensures schema.
// Fatal logs on error in order to keep the previous signature without error return.
func Load(path string) *Store {
	db, err := sqlx.Open("sqlite3", path)
	if err != nil {
		logx.Fatal("open sqlite db", "path", path, "err", err)
	}
	// A small busy timeout to reduce lock errors under light concurrent access.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		logx.Warn("sqlite pragma busy_timeout", "err", err)
	}
	if err := ensureSchema(db); err != nil {
		logx.Fatal("init schema", "err", err)
	}
	return &Store{db: db}
}

func ensureSchema(db *sqlx.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS guild_settings (
            guild_id   TEXT PRIMARY KEY,
            channel_id TEXT,
            timezone   TEXT,
            enabled    INTEGER,
            org        TEXT,
            run_hour   INTEGER
        );
        CREATE TABLE IF NOT EXISTS last_posted (
            guild_id  TEXT NOT NULL,
            sport     TEXT NOT NULL,
            last_date TEXT NOT NULL,
            PRIMARY KEY (guild_id, sport)
        );
    `)
	if err != nil {
		return err
	}
	// Best-effort migration: add columns if upgrading from older schema.
	// SQLite may error if the column already exists; ignore such errors.
	if _, err := db.Exec("ALTER TABLE guild_settings ADD COLUMN enabled INTEGER"); err != nil {
		// ignore
	}
	if _, err := db.Exec("ALTER TABLE guild_settings ADD COLUMN org TEXT"); err != nil {
		// ignore
	}
	if _, err := db.Exec("ALTER TABLE guild_settings ADD COLUMN run_hour INTEGER"); err != nil {
		// ignore
	}
	return nil
}

// Save is a no-op for the SQLite-backed store and exists for backward compatibility.
func (s *Store) Save(_ string) error { return nil }

// GuildIDs returns the set of guild IDs with settings persisted.
func (s *Store) GuildIDs() []string {
	var ids []string
	if err := s.db.Select(&ids, "SELECT guild_id FROM guild_settings"); err != nil {
		logx.Error("state: list guild ids", "err", err)
		return nil
	}
	return ids
}

// GetGuildSettings returns channel, timezone, and last-posted map for the guild.
func (s *Store) GetGuildSettings(guildID string) (channelID, tz string, lastPosted map[string]string) {
	// settings
	row := s.db.QueryRowx("SELECT COALESCE(channel_id, ''), COALESCE(timezone, '') FROM guild_settings WHERE guild_id = ?", guildID)
	_ = row.Scan(&channelID, &tz)
	// last_posted
	lastPosted = make(map[string]string)
	rows, err := s.db.Queryx("SELECT sport, last_date FROM last_posted WHERE guild_id = ?", guildID)
	if err != nil {
		return channelID, tz, lastPosted
	}
	defer rows.Close()
	for rows.Next() {
		var sport, date string
		if err := rows.Scan(&sport, &date); err == nil {
			lastPosted[sport] = date
		}
	}
	return channelID, tz, lastPosted
}

// UpdateGuildChannel upserts the desired channel for the guild.
func (s *Store) UpdateGuildChannel(guildID, channelID string) {
	// Ensure row then update specific column to avoid clobbering other fields.
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		logx.Error("state: ensure guild", "guild_id", guildID, "err", err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET channel_id = ? WHERE guild_id = ?", channelID, guildID); err != nil {
		logx.Error("state: update channel", "guild_id", guildID, "err", err)
	}
}

// UpdateGuildTZ upserts the timezone for the guild.
func (s *Store) UpdateGuildTZ(guildID, tz string) {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		logx.Error("state: ensure guild", "guild_id", guildID, "err", err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET timezone = ? WHERE guild_id = ?", tz, guildID); err != nil {
		logx.Error("state: update timezone", "guild_id", guildID, "err", err)
	}
}

// MarkPosted records the most recent YYYY-MM-DD date a notification was posted for a sport.
func (s *Store) MarkPosted(guildID, sport, yyyyMmDd string) {
	if _, err := s.db.Exec(
		"INSERT INTO last_posted (guild_id, sport, last_date) VALUES (?, ?, ?) "+
			"ON CONFLICT(guild_id, sport) DO UPDATE SET last_date = excluded.last_date",
		guildID, sport, yyyyMmDd,
	); err != nil {
		logx.Error("state: mark posted", "guild_id", guildID, "sport", sport, "err", err)
	}
}

// UpdateGuildNotifyEnabled upserts the notify enabled flag for the guild.
func (s *Store) UpdateGuildNotifyEnabled(guildID string, enabled bool) {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		logx.Error("state: ensure guild", "guild_id", guildID, "err", err)
		return
	}
	val := 0
	if enabled {
		val = 1
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET enabled = ? WHERE guild_id = ?", val, guildID); err != nil {
		logx.Error("state: update enabled", "guild_id", guildID, "err", err)
	}
}

// GetGuildNotifyEnabled returns true if notifications are enabled (default true when unset).
func (s *Store) GetGuildNotifyEnabled(guildID string) bool {
	var enabled sql.NullInt32
	row := s.db.QueryRowx("SELECT enabled FROM guild_settings WHERE guild_id = ?", guildID)
	_ = row.Scan(&enabled)
	// Default: notifications OFF until explicitly enabled.
	if !enabled.Valid {
		return false
	}
	return enabled.Int32 != 0
}

// UpdateGuildOrg upserts the org for the guild.
func (s *Store) UpdateGuildOrg(guildID, org string) {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		logx.Error("state: ensure guild", "guild_id", guildID, "err", err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET org = ? WHERE guild_id = ?", org, guildID); err != nil {
		logx.Error("state: update org", "guild_id", guildID, "err", err)
	}
}

// GetGuildOrg returns the selected org for the guild (default "ufc").
func (s *Store) GetGuildOrg(guildID string) string {
	var org string
	row := s.db.QueryRowx("SELECT org FROM guild_settings WHERE guild_id = ?", guildID)
	_ = row.Scan(&org)
	if org == "" {
		return "ufc"
	}
	return org
}

// HasGuildOrg returns true if an org has been explicitly set.
func (s *Store) HasGuildOrg(guildID string) bool {
	var org sql.NullString
	row := s.db.QueryRowx("SELECT org FROM guild_settings WHERE guild_id = ?", guildID)
	_ = row.Scan(&org)
	return org.Valid && org.String != ""
}

// UpdateGuildRunAt upserts the run-at time (HH:MM) for the guild.
// (run_at removed) Per-guild minute precision is not stored; use env RUN_AT for default.

// UpdateGuildRunHour upserts the run hour (0-23) for the guild.
func (s *Store) UpdateGuildRunHour(guildID string, hour int) {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		logx.Error("state: ensure guild", "guild_id", guildID, "err", err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET run_hour = ? WHERE guild_id = ?", hour, guildID); err != nil {
		logx.Error("state: update run_hour", "guild_id", guildID, "err", err)
	}
}

// GetGuildRunHour returns the configured hour (0-23) or -1 when unset.
func (s *Store) GetGuildRunHour(guildID string) int {
	var hour sql.NullInt32
	row := s.db.QueryRowx("SELECT run_hour FROM guild_settings WHERE guild_id = ?", guildID)
	_ = row.Scan(&hour)
	if !hour.Valid {
		return -1
	}
	return int(hour.Int32)
}
