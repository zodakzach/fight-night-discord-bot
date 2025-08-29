package migrate

import (
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	iofs "github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
)

// Embed all SQL migration files.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Run applies all up migrations against the SQLite database at path.
// It is safe to call repeatedly; no-op when already up-to-date.
func Run(path string) error {
	// Open using same driver as the app to ensure identical behavior.
	db, err := sqlx.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("open sqlite db %q: %w", path, err)
	}
	defer db.Close()

	// Keep consistent with the rest of the app; non-fatal if it fails.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		logx.Warn("sqlite pragma busy_timeout (migrate)", "err", err)
	}

	// Database driver instance for golang-migrate.
	driver, err := sqlite3.WithInstance(db.DB, &sqlite3.Config{})
	if err != nil {
		return fmt.Errorf("sqlite migrate driver: %w", err)
	}

	// Source driver using embedded files.
	src, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
