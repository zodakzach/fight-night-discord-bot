package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
)

const (
	DefaultTZ    = "America/New_York"
	DefaultRunAt = "16:00" // HH:MM process-local time for daily check
	// Default SQLite DB file path for persistent state
	DefaultDBFile = "state.db"
)

type Config struct {
	Token string

	RunAt     string
	StatePath string
	TZ        string
	DevGuild  string
	UserAgent string
}

func Load() Config {
	// Load environment variables from a .env file if present.
	// Non-fatal: proceed if the file is missing so production env vars still work.
	if err := godotenv.Load(); err != nil {
		// Informational only when missing; production often omits .env.
		logx.Debug("godotenv load", "err", err)
	}

	// Use DB_FILE, defaulting to a local SQLite file.
	dbPath := getEnv("DB_FILE", DefaultDBFile)
	return Config{
		Token:     mustEnv("DISCORD_TOKEN"),
		RunAt:     getEnv("RUN_AT", DefaultRunAt),
		StatePath: dbPath,
		TZ:        getEnv("TZ", DefaultTZ),
		DevGuild:  os.Getenv("GUILD_ID"),
		UserAgent: getEnv("USER_AGENT", "ufc-fight-night-notifier/1.0 (contact: zach@codeezy.dev)"),
	}
}

func getEnv(k, def string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		logx.Fatal("missing required env var", "key", k)
	}
	return v
}
