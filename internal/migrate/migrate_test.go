package migrate

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type colInfo struct {
	Cid        int    `db:"cid"`
	Name       string `db:"name"`
	Type       string `db:"type"`
	NotNull    int    `db:"notnull"`
	DefaultVal any    `db:"dflt_value"`
	PK         int    `db:"pk"`
}

func tableInfo(t *testing.T, db *sqlx.DB, table string) []colInfo {
	t.Helper()
	rows := []colInfo{}
	if err := db.Select(&rows, "PRAGMA table_info("+table+")"); err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	return rows
}

func TestRun_AppliesInitialSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	if err := Run(dbPath); err != nil {
		t.Fatalf("migrate run: %v", err)
	}

	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// guild_settings columns
	gs := tableInfo(t, db, "guild_settings")
	if len(gs) != 6 {
		t.Fatalf("guild_settings columns: got %d", len(gs))
	}
	wantGs := map[string]struct {
		typ string
		pk  bool
	}{
		"guild_id":   {typ: "TEXT", pk: true},
		"channel_id": {typ: "TEXT", pk: false},
		"timezone":   {typ: "TEXT", pk: false},
		"enabled":    {typ: "INTEGER", pk: false},
		"org":        {typ: "TEXT", pk: false},
		"run_hour":   {typ: "INTEGER", pk: false},
	}
	for _, c := range gs {
		w, ok := wantGs[c.Name]
		if !ok {
			t.Fatalf("unexpected column in guild_settings: %q", c.Name)
		}
		if c.Type != w.typ {
			t.Fatalf("guild_settings.%s type: got %q want %q", c.Name, c.Type, w.typ)
		}
		if (c.PK != 0) != w.pk {
			t.Fatalf("guild_settings.%s pk: got %v want %v", c.Name, c.PK != 0, w.pk)
		}
	}

	// last_posted columns
	lp := tableInfo(t, db, "last_posted")
	if len(lp) != 3 {
		t.Fatalf("last_posted columns: got %d", len(lp))
	}
	wantLp := map[string]struct {
		typ string
		pk  bool
	}{
		"guild_id":  {typ: "TEXT", pk: true},
		"sport":     {typ: "TEXT", pk: true},
		"last_date": {typ: "TEXT", pk: false},
	}
	for _, c := range lp {
		w, ok := wantLp[c.Name]
		if !ok {
			t.Fatalf("unexpected column in last_posted: %q", c.Name)
		}
		if c.Type != w.typ {
			t.Fatalf("last_posted.%s type: got %q want %q", c.Name, c.Type, w.typ)
		}
		if (c.PK != 0) != w.pk {
			t.Fatalf("last_posted.%s pk: got %v want %v", c.Name, c.PK != 0, w.pk)
		}
	}
}

func TestRun_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	if err := Run(dbPath); err != nil {
		t.Fatalf("first migrate run: %v", err)
	}
	if err := Run(dbPath); err != nil { // no-op when up-to-date
		t.Fatalf("second migrate run (idempotent): %v", err)
	}

	// Sanity: open and do a simple write to ensure DB is usable post-migration
	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT OR IGNORE INTO guild_settings (guild_id, channel_id) VALUES (?, ?)", "g1", "c1"); err != nil {
		t.Fatalf("insert after migration: %v", err)
	}
}
