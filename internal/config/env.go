package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultTZ        = "America/New_York"
	DefaultRunAt     = "16:00" // HH:MM process-local time for daily check
	DefaultStateFile = "state.json"
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
		// Only log at info level to avoid failing when no .env is provided.
		log.Printf("godotenv: %v", err)
	}

	return Config{
		Token:     mustEnv("DISCORD_TOKEN"),
		RunAt:     getEnv("RUN_AT", DefaultRunAt),
		StatePath: getEnv("STATE_FILE", DefaultStateFile),
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
		log.Fatalf("Missing env var: %s", k)
	}
	return v
}
