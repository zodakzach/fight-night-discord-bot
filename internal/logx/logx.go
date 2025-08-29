package logx

import (
	"log/slog"
	"os"
	"strings"
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
func Error(msg string, kv ...any) { defaultLogger.Error(msg, kv...) }

// Fatal logs an error and exits the process with code 1 (no stack trace).
func Fatal(msg string, kv ...any) {
	defaultLogger.Error(msg, kv...)
	os.Exit(1)
}
