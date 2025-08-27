package state

import (
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
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
		log.Fatalf("open sqlite db %q: %v", path, err)
	}
	// A small busy timeout to reduce lock errors under light concurrent access.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		log.Printf("sqlite pragma busy_timeout: %v", err)
	}
	if err := ensureSchema(db); err != nil {
		log.Fatalf("init schema: %v", err)
	}
	return &Store{db: db}
}

func ensureSchema(db *sqlx.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS guild_settings (
            guild_id   TEXT PRIMARY KEY,
            channel_id TEXT,
            timezone   TEXT
        );
        CREATE TABLE IF NOT EXISTS last_posted (
            guild_id  TEXT NOT NULL,
            sport     TEXT NOT NULL,
            last_date TEXT NOT NULL,
            PRIMARY KEY (guild_id, sport)
        );
    `)
	return err
}

// Save is a no-op for the SQLite-backed store and exists for backward compatibility.
func (s *Store) Save(_ string) error { return nil }

// GuildIDs returns the set of guild IDs with settings persisted.
func (s *Store) GuildIDs() []string {
	var ids []string
	if err := s.db.Select(&ids, "SELECT guild_id FROM guild_settings"); err != nil {
		log.Printf("state: list guild ids: %v", err)
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
		log.Printf("state: ensure guild %s: %v", guildID, err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET channel_id = ? WHERE guild_id = ?", channelID, guildID); err != nil {
		log.Printf("state: update channel for %s: %v", guildID, err)
	}
}

// UpdateGuildTZ upserts the timezone for the guild.
func (s *Store) UpdateGuildTZ(guildID, tz string) {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id) VALUES (?)", guildID); err != nil {
		log.Printf("state: ensure guild %s: %v", guildID, err)
		return
	}
	if _, err := s.db.Exec("UPDATE guild_settings SET timezone = ? WHERE guild_id = ?", tz, guildID); err != nil {
		log.Printf("state: update tz for %s: %v", guildID, err)
	}
}

// MarkPosted records the most recent YYYY-MM-DD date a notification was posted for a sport.
func (s *Store) MarkPosted(guildID, sport, yyyyMmDd string) {
	if _, err := s.db.Exec(
		"INSERT INTO last_posted (guild_id, sport, last_date) VALUES (?, ?, ?) "+
			"ON CONFLICT(guild_id, sport) DO UPDATE SET last_date = excluded.last_date",
		guildID, sport, yyyyMmDd,
	); err != nil {
		log.Printf("state: mark posted for %s/%s: %v", guildID, sport, err)
	}
}
