package logx

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/sentryx"
)

var defaultLogger *slog.Logger

// Ensure a safe default logger is available even if Init isn't called.
// This prevents nil-pointer panics during tests or early package use.
func init() {
	if defaultLogger == nil {
		h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
		l := slog.New(h)
		defaultLogger = l
		slog.SetDefault(l)
	}
}

// Init configures a JSON structured logger suitable for Fly.io log ingestion.
// It reads LOG_LEVEL (debug, info, warn, error) and sets a global default.
func Init(service string) {
	level := parseLevel(getenv("LOG_LEVEL", "info"))
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	l := slog.New(handler).With(
		slog.String("service", service),
	)
	defaultLogger = l
	slog.SetDefault(l)
}

func parseLevel(s string) slog.Leveler {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// Debug logs at debug level with structured fields.
func Debug(msg string, kv ...any) { defaultLogger.Debug(msg, kv...) }

// Info logs at info level with structured fields.
func Info(msg string, kv ...any) { defaultLogger.Info(msg, kv...) }

// Warn logs at warn level with structured fields.
func Warn(msg string, kv ...any) { defaultLogger.Warn(msg, kv...) }

// Error logs at error level with structured fields.
func Error(msg string, kv ...any) {
	defaultLogger.Error(msg, kv...)
	if err := extractErr(kv...); err != nil {
		sentryx.CaptureException(err, buildExtras(msg, kv...))
	}
}

// Fatal logs an error and exits the process with code 1 (no stack trace).
func Fatal(msg string, kv ...any) {
	// Log to stdout/stderr via slog first
	defaultLogger.Error(msg, kv...)

	// Send to Sentry if configured
	if err := extractErr(kv...); err != nil {
		sentryx.CaptureException(err, buildExtras(msg, kv...))
	} else {
		extra := buildExtras(msg, kv...)
		extra["level"] = "fatal"
		sentryx.CaptureMessage(msg, extra)
	}
	// Best-effort flush for in-flight events
	sentryx.Flush(2 * time.Second)
	os.Exit(1)
}

// Measure returns a closure that logs the elapsed time since creation
// when invoked. Typical usage:
//
//	done := logx.Measure("fetch ufc scoreboard", "dates", dates)
//	defer done()
//
// You can pass more fields at call time, which are merged into the log entry.
func Measure(msg string, kv ...any) func(more ...any) {
	start := time.Now()
	return func(more ...any) {
		elapsed := time.Since(start)
		// Merge original kv, duration, and any additional fields
		all := append([]any{}, kv...)
		all = append(all, "duration_ms", elapsed.Milliseconds())
		if len(more) > 0 {
			all = append(all, more...)
		}
		defaultLogger.Info(msg, all...)
	}
}

// MeasureDebug is like Measure but logs at debug level.
func MeasureDebug(msg string, kv ...any) func(more ...any) {
	start := time.Now()
	return func(more ...any) {
		elapsed := time.Since(start)
		all := append([]any{}, kv...)
		all = append(all, "duration_ms", elapsed.Milliseconds())
		if len(more) > 0 {
			all = append(all, more...)
		}
		defaultLogger.Debug(msg, all...)
	}
}

// extractErr looks for a key named "err" and returns it if it's an error.
func extractErr(kv ...any) error {
	// Expect alternating key/value pairs
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok && key == "err" {
			if e, ok := kv[i+1].(error); ok {
				return e
			}
		}
	}
	return nil
}

// buildExtras converts key/value pairs to a Sentry extras map.
func buildExtras(msg string, kv ...any) map[string]any {
	extras := map[string]any{"message": msg}
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok || key == "err" {
			continue
		}
		extras[key] = kv[i+1]
	}
	return extras
}
