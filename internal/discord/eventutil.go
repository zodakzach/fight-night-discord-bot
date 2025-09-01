package discord

import (
	"context"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
)

// getNextEventFunc allows tests to stub the provider-driven next-event lookup.
// Returns the normalized event if found.
var getNextEventFunc = func(ctx context.Context, p sources.Provider) (*sources.Event, bool, error) {
	return p.NextEvent(ctx)
}

// pickNextEvent uses the Provider to select the ongoing or next event and returns
// the normalized event for downstream display/embeds.
func pickNextEvent(ctx context.Context, p sources.Provider) (*sources.Event, bool, error) {
	return getNextEventFunc(ctx, p)
}
