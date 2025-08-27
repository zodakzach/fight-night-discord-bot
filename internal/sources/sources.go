package sources

import (
	"context"
	"net/http"

	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
)

// Event is a normalized MMA event used by higher-level features.
type Event struct {
	ID        string
	Name      string
	ShortName string
	Date      string // RFC3339
}

// Provider fetches events for a specific organization over a range.
type Provider interface {
	FetchEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]Event, error)
}

// Manager resolves a Provider for a given org key (e.g., "ufc").
type Manager struct {
	providers map[string]Provider
}

// NewManager creates an empty manager; register providers via Register.
func NewManager() *Manager { return &Manager{providers: make(map[string]Provider)} }

// Register associates an org key with a provider.
func (m *Manager) Register(org string, p Provider) { m.providers[org] = p }

// Provider returns the registered provider for org, if any.
func (m *Manager) Provider(org string) (Provider, bool) {
	p, ok := m.providers[org]
	return p, ok
}

// NewDefaultManager wires built-in providers for known orgs.
// Today this registers UFC via the ESPN scraper client.
func NewDefaultManager(httpc *http.Client, userAgent string) *Manager {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	m := NewManager()
	// UFC via ESPN client adapter
	m.Register("ufc", &ufcProvider{c: espn.NewClient(httpc, userAgent)})
	return m
}

// ufcProvider adapts the ESPN client to the generic Provider interface.
type ufcProvider struct{ c *espn.HTTPClient }

func (p *ufcProvider) FetchEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]Event, error) {
	evs, err := p.c.FetchUFCEventsRange(ctx, startYYYYMMDD, endYYYYMMDD)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, Event{ID: e.ID, Name: e.Name, ShortName: e.ShortName, Date: e.Date})
	}
	return out, nil
}
