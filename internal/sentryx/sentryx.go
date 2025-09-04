package sentryx

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

var enabled bool

// InitFromEnv initializes Sentry when SENTRY_DSN is provided.
// Non-fatal if DSN is empty; logging continues without Sentry.
func InitFromEnv(service string) error {
	dsn := strings.TrimSpace(os.Getenv("SENTRY_DSN"))
	if dsn == "" {
		enabled = false
		return nil
	}

	env := firstNonEmpty(
		os.Getenv("SENTRY_ENV"),
		os.Getenv("SENTRY_ENVIRONMENT"),
		os.Getenv("ENVIRONMENT"),
		os.Getenv("ENV"),
	)
	if env == "" {
		env = "production"
	}

	// Optional: traces sample rate for performance monitoring.
	var tracesSample float64
	if v := strings.TrimSpace(os.Getenv("SENTRY_TRACES_SAMPLE_RATE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			tracesSample = f
		}
	}

	opts := sentry.ClientOptions{
		Dsn:         dsn,
		Environment: env,
		ServerName:  service,
	}
	if tracesSample > 0 {
		opts.TracesSampleRate = tracesSample
	}

	if err := sentry.Init(opts); err != nil {
		enabled = false
		return err
	}
	enabled = true
	// Attach a default tag for service to all events
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("service", service)
	})
	return nil
}

// CaptureException sends an error to Sentry with optional extra fields.
func CaptureException(err error, extra map[string]any) {
	if !enabled || err == nil {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range extra {
			scope.SetExtra(k, v)
		}
		sentry.CaptureException(err)
	})
}

// CaptureMessage sends a message to Sentry when enabled.
func CaptureMessage(msg string, extra map[string]any) {
	if !enabled || strings.TrimSpace(msg) == "" {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		for k, v := range extra {
			scope.SetExtra(k, v)
		}
		sentry.CaptureMessage(msg)
	})
}

// Recover captures panics and re-panics.
func Recover() {
	if r := recover(); r != nil {
		if enabled {
			sentry.CurrentHub().Recover(r)
			sentry.Flush(2 * time.Second)
		}
		panic(r)
	}
}

// Flush ensures buffered events are sent.
func Flush(timeout time.Duration) {
	if !enabled {
		return
	}
	sentry.Flush(timeout)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
