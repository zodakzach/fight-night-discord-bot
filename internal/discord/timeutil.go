package discord

import (
	"fmt"
	"time"
)

// parseAPITime parses known API time layouts, falling back across several
// RFC3339 variants commonly returned by upstream services.
func parseAPITime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04Z07:00",   // no seconds (sample)
		time.RFC3339,               // with seconds
		time.RFC3339Nano,           // with fractional seconds
		"2006-01-02T15:04:05Z0700", // no colon in offset
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", s)
}
