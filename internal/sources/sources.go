package sources

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
)

// Event is a normalized MMA event used by higher-level features.
type Event struct {
	ID        string
	Name      string
	ShortName string
	Date      string // RFC3339
}

// Provider fetches events for a specific organization and exposes next-event.
type Provider interface {
	// NextEvent returns the next or ongoing event; time is RFC3339 UTC.
	NextEvent(ctx context.Context) (name string, atUTC string, ok bool, err error)
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

// Orgs returns a sorted list of registered organization keys.
func (m *Manager) Orgs() []string {
	keys := make([]string, 0, len(m.providers))
	for k := range m.providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// NewDefaultManager wires built-in providers for known orgs.
// Today this registers UFC via the ESPN client adapter.
func NewDefaultManager(httpc *http.Client, userAgent string) *Manager {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	m := NewManager()
	m.Register("ufc", &ufcProvider{c: espn.NewClient(httpc, userAgent)})
	return m
}

// ufcProvider adapts the ESPN client to the generic Provider interface.
type ufcProvider struct{ c *espn.HTTPClient }

func (p *ufcProvider) NextEvent(ctx context.Context) (string, string, bool, error) {
	// Selection strictly in UTC; conversion happens in discord/eventutil.
	// Default behavior: ignore Contender Series unless context overrides.
	ignores := []string{"Contender Series"}
	if ignore, ok := ufcIgnoreContenderFromContext(ctx); ok {
		if !ignore {
			ignores = nil
		}
	}
	ev, _, stUTC, _, ok, err := p.c.FetchNextOrOngoingEventAndCard(ctx, ignores, time.Now)
	if err != nil || !ok || ev == nil {
		if err != nil {
			return "", "", false, err
		}
		return "", "", false, nil
	}
	name := ev.Name
	if name == "" {
		name = ev.ShortName
	}
	return name, stUTC.UTC().Format(time.RFC3339), true, nil
}

// ---- Context options for provider behavior ----

type ctxKey int

const (
	ctxKeyUFCIgnoreContender ctxKey = iota
)

// WithUFCIgnoreContender annotates ctx with whether to ignore Contender Series
// when selecting the next UFC event. If not set, providers default to ignoring.
func WithUFCIgnoreContender(ctx context.Context, ignore bool) context.Context {
	return context.WithValue(ctx, ctxKeyUFCIgnoreContender, ignore)
}

func ufcIgnoreContenderFromContext(ctx context.Context) (bool, bool) {
	v := ctx.Value(ctxKeyUFCIgnoreContender)
	if v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}
