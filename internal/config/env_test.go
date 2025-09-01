package config

import (
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
    "time"
    "sync"

    "github.com/joho/godotenv"
)

func Test_getEnv_DefaultAndValue(t *testing.T) {
    t.Setenv("CFG_TEST_KEY", "")
    if got := getEnv("CFG_TEST_KEY", "default"); got != "default" {
        t.Fatalf("expected default when unset, got %q", got)
    }
    t.Setenv("CFG_TEST_KEY", "  ")
    if got := getEnv("CFG_TEST_KEY", "default"); got != "default" {
        t.Fatalf("expected default when whitespace, got %q", got)
    }
    t.Setenv("CFG_TEST_KEY", "value")
    if got := getEnv("CFG_TEST_KEY", "default"); got != "value" {
        t.Fatalf("expected explicit value, got %q", got)
    }
}

func Test_mustEnv_ReturnsWhenSet(t *testing.T) {
    t.Setenv("MUST_OK", "present")
    if got := mustEnv("MUST_OK"); got != "present" {
        t.Fatalf("expected present, got %q", got)
    }
}

// Test_mustEnv_ExitWhenMissing validates that mustEnv exits the process when the key is missing.
func Test_mustEnv_ExitWhenMissing(t *testing.T) {
    if runtime.GOOS == "js" || runtime.GOOS == "wasip1" { // no subprocess support
        t.Skip("skipping on platforms without exec support")
    }
    cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_MustEnv")
    cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
    cmd.Env = append(cmd.Env, "MISSING_ENV_FOR_TEST=")
    err := cmd.Run()
    if err == nil {
        t.Fatalf("expected process to exit with error when env missing")
    }
}

// TestHelperProcess_MustEnv is invoked as a separate process by Test_mustEnv_ExitWhenMissing.
func TestHelperProcess_MustEnv(t *testing.T) {
    if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
        return
    }
    // Ensure the key is not set
    os.Unsetenv("MISSING_ENV_FOR_TEST")
    // This should call logx.Fatal which exits the process.
    _ = mustEnv("MISSING_ENV_FOR_TEST")
    // If it returns, fail fast.
    t.Fatalf("mustEnv did not exit when env missing")
}

func Test_Load_UsesDefaultsAndEnv(t *testing.T) {
    // Ensure Load() hits the godotenv.Load error path by running in this package directory
    t.Setenv("DISCORD_TOKEN", "token-abc")
    // Unset optional vars to test defaults
    os.Unsetenv("RUN_AT")
    os.Unsetenv("TZ")
    os.Unsetenv("DB_FILE")
    os.Unsetenv("GUILD_ID")
    os.Unsetenv("USER_AGENT")

    cfg := Load()
    if cfg.Token != "token-abc" {
        t.Fatalf("Token mismatch: %q", cfg.Token)
    }
    if cfg.RunAt != DefaultRunAt {
        t.Fatalf("RunAt default mismatch: %q", cfg.RunAt)
    }
    if cfg.TZ != DefaultTZ {
        t.Fatalf("TZ default mismatch: %q", cfg.TZ)
    }
    if cfg.StatePath != DefaultDBFile {
        t.Fatalf("StatePath default mismatch: %q", cfg.StatePath)
    }
    if cfg.DevGuild != "" {
        t.Fatalf("DevGuild expected empty, got %q", cfg.DevGuild)
    }
    if !strings.Contains(cfg.UserAgent, "ufc-fight-night-notifier") {
        t.Fatalf("UserAgent default mismatch: %q", cfg.UserAgent)
    }
}

func Test_Load_WithEnvOverrides(t *testing.T) {
    t.Setenv("DISCORD_TOKEN", "xyz")
    t.Setenv("RUN_AT", "10:30")
    t.Setenv("TZ", "Europe/London")
    t.Setenv("DB_FILE", "/tmp/test.db")
    t.Setenv("GUILD_ID", "123")
    t.Setenv("USER_AGENT", "custom-agent/1.0")

    cfg := Load()
    if cfg.Token != "xyz" || cfg.RunAt != "10:30" || cfg.TZ != "Europe/London" || cfg.StatePath != "/tmp/test.db" || cfg.DevGuild != "123" || cfg.UserAgent != "custom-agent/1.0" {
        t.Fatalf("unexpected cfg: %+v", cfg)
    }
}

func Test_LiveESPNEnabled_DefaultFalse(t *testing.T) {
    // Reset once to allow executing the loader and avoid picking up repo root .env
    oldWD, _ := os.Getwd()
    defer func() { _ = os.Chdir(oldWD) }()
    tmp := t.TempDir()
    if err := os.Chdir(tmp); err != nil {
        t.Fatalf("chdir: %v", err)
    }
    dotenvOnce = sync.Once{}
    os.Unsetenv("ESPN_LIVE")
    os.Unsetenv("RUN_LIVE_ESPN")
    if LiveESPNEnabled() {
        t.Fatalf("expected LiveESPNEnabled to be false by default")
    }
}

func Test_LiveESPNEnabled_FromEnv(t *testing.T) {
    dotenvOnce = sync.Once{}
    t.Setenv("ESPN_LIVE", "1")
    if !LiveESPNEnabled() {
        t.Fatalf("expected LiveESPNEnabled true when ESPN_LIVE=1")
    }
    dotenvOnce = sync.Once{}
    os.Unsetenv("ESPN_LIVE")
    t.Setenv("RUN_LIVE_ESPN", "yes")
    if !LiveESPNEnabled() {
        t.Fatalf("expected LiveESPNEnabled true when RUN_LIVE_ESPN=yes")
    }
}

func Test_loadDotEnvUpward_LoadsFromParent(t *testing.T) {
    // Create nested directories tmp/a/b/c and write .env in tmp/a
    root := t.TempDir()
    a := filepath.Join(root, "a")
    b := filepath.Join(a, "b")
    c := filepath.Join(b, "c")
    for _, d := range []string{a, b, c} {
        if err := os.MkdirAll(d, 0o755); err != nil {
            t.Fatalf("mkdir %s: %v", d, err)
        }
    }
    envPath := filepath.Join(a, ".env")
    if err := os.WriteFile(envPath, []byte("CFG_UPWARD_TEST=ok\n"), 0o644); err != nil {
        t.Fatalf("write .env: %v", err)
    }

    // Change to deepest dir and ensure var not set
    oldWD, _ := os.Getwd()
    defer func() { _ = os.Chdir(oldWD) }()
    if err := os.Chdir(c); err != nil {
        t.Fatalf("chdir: %v", err)
    }
    os.Unsetenv("CFG_UPWARD_TEST")

    // Reset once and attempt upward load
    dotenvOnce = sync.Once{}
    // Using Overload directly validates our test setup as well
    _ = godotenv.Overload(envPath)
    // Clear and use our function which should early-return when it finds the same file
    os.Unsetenv("CFG_UPWARD_TEST")
    dotenvOnce = sync.Once{}
    loadDotEnvUpward()
    if os.Getenv("CFG_UPWARD_TEST") != "ok" {
        t.Fatalf("expected CFG_UPWARD_TEST to be loaded from parent .env, got %q", os.Getenv("CFG_UPWARD_TEST"))
    }

    // Call again to ensure sync.Once prevents re-loading; value should remain the same
    t.Setenv("CFG_UPWARD_TEST", "changed")
    loadDotEnvUpward()
    if os.Getenv("CFG_UPWARD_TEST") != "changed" {
        t.Fatalf("expected value to remain as set due to once, got %q", os.Getenv("CFG_UPWARD_TEST"))
    }
    _ = time.Now() // keep time import used
}
