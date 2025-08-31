package discord

import (
	"context"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
)

// getNextEventFunc allows tests to stub the provider-driven next-event lookup.
var getNextEventFunc = func(ctx context.Context, p sources.Provider, loc *time.Location) (string, time.Time, bool, error) {
	name, atUTC, ok, err := p.NextEvent(ctx)
	if err != nil || !ok {
		if err != nil {
			return "", time.Time{}, false, err
		}
		return "", time.Time{}, false, nil
	}
	t, err := parseAPITime(atUTC)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return name, t.In(loc), true, nil
}

// pickNextEvent uses the Provider to select the ongoing or next event and returns
// the event name and the start time converted into the given location for display.
func pickNextEvent(ctx context.Context, p sources.Provider, loc *time.Location) (string, time.Time, bool, error) {
	return getNextEventFunc(ctx, p, loc)
}
