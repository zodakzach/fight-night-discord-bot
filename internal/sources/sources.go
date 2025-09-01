package sources

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
)

// Link represents an external link related to an event (e.g., ESPN page).
type Link struct {
	Title string
	URL   string
}

// Bout is a normalized fight within an event card.
type Bout struct {
	WeightClass string
	RedName     string
	RedRecord   string
	BlueName    string
	BlueRecord  string
	Winner      string
	// Scheduled is RFC3339 UTC if known
	Scheduled string
}

// Event is the bot's normalized representation for an MMA event across orgs.
// All times are RFC3339 in UTC; presentation layers convert to a guild TZ.
type Event struct {
	Org       string
	ID        string
	Name      string
	ShortName string
	Start     string // RFC3339 UTC
	End       string // RFC3339 UTC (may be empty)
	BannerURL string // Optional image to use in embeds
	Links     []Link
	Bouts     []Bout
}

// Provider fetches events for a specific organization and exposes next-event.
type Provider interface {
	// NextEvent returns the next or ongoing event normalized to the Event type.
	NextEvent(ctx context.Context) (*Event, bool, error)
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

func (p *ufcProvider) NextEvent(ctx context.Context) (*Event, bool, error) {
	// Selection strictly in UTC; conversion happens in discord/eventutil.
	// Default behavior: ignore Contender Series unless context overrides.
	ignores := []string{"Contender Series"}
	if ignore, ok := ufcIgnoreContenderFromContext(ctx); ok {
		if !ignore {
			ignores = nil
		}
	}
	ev, fights, stUTC, enUTC, ok, err := p.c.FetchNextOrOngoingEventAndCard(ctx, ignores, time.Now)
	if err != nil || !ok || ev == nil {
		if err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	name := ev.Name
	if name == "" {
		name = ev.ShortName
	}
	// Map ESPN fights to normalized bouts
	bouts := make([]Bout, 0, len(fights))
	for _, f := range fights {
		sched := ""
		if !f.Scheduled.IsZero() {
			sched = f.Scheduled.UTC().Format(time.RFC3339)
		}
		bouts = append(bouts, Bout{
			WeightClass: f.WeightClass,
			RedName:     f.RedName,
			RedRecord:   f.RedRecord,
			BlueName:    f.BlueName,
			BlueRecord:  f.BlueRecord,
			Winner:      f.Winner,
			Scheduled:   sched,
		})
	}
	// Map links where available with friendlier titles
	links := make([]Link, 0, len(ev.Links))
	for _, l := range ev.Links {
		if l.Href == "" {
			continue
		}
		raw := firstNonEmpty(l.Text, l.ShortText)
		title := raw
		if strings.EqualFold(strings.TrimSpace(raw), "gamecast") {
			title = "Event Page"
		}
		if strings.TrimSpace(title) == "" {
			title = "Link"
		}
		links = append(links, Link{Title: title, URL: l.Href})
	}
	// Attempt to pick a banner image from event logos when available
	banner := ""
	if len(ev.Logos) > 0 && strings.TrimSpace(ev.Logos[0].Href) != "" {
		banner = ev.Logos[0].Href
	}
	start := stUTC.UTC().Format(time.RFC3339)
	end := ""
	if !enUTC.IsZero() {
		end = enUTC.UTC().Format(time.RFC3339)
	}
	out := &Event{
		Org:       "ufc",
		ID:        ev.ID,
		Name:      name,
		ShortName: ev.ShortName,
		Start:     start,
		End:       end,
		BannerURL: banner,
		Links:     links,
		Bouts:     bouts,
	}
	return out, true, nil
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

// firstNonEmpty returns the first non-empty (after trimming) string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
