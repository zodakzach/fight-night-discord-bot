package discord

import (
	"context"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
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

// providerForGuild returns the org key, provider, and context (with any per-org
// options applied) for a guild. When defaultToUFC is true, it will fall back to
// "ufc" when no org is set in state.
func providerForGuild(st *state.Store, mgr *sources.Manager, guildID string, defaultToUFC bool) (string, sources.Provider, context.Context, bool) {
	org := st.GetGuildOrg(guildID)
	if org == "" && defaultToUFC {
		org = "ufc"
	}
	if org == "" {
		return "", nil, context.Background(), false
	}
	p, ok := mgr.Provider(org)
	if !ok {
		return org, nil, context.Background(), false
	}
	ctx := context.Background()
	if org == "ufc" {
		ctx = sources.WithUFCIgnoreContender(ctx, st.GetGuildUFCIgnoreContender(guildID))
	}
	return org, p, ctx, true
}
