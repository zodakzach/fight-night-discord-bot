package sources

import (
	"context"
	"testing"

	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
)

// fakeESPNClient implements espn.Client for testing the UFC adapter.
type fakeESPNClient struct {
	gotStart string
	gotEnd   string
	events   []espn.Event
	err      error
}

func (f *fakeESPNClient) FetchUFCEvents(ctx context.Context, yyyymmdd string) ([]espn.Event, error) {
	return nil, nil
}

func (f *fakeESPNClient) FetchUFCEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]espn.Event, error) {
	f.gotStart = startYYYYMMDD
	f.gotEnd = endYYYYMMDD
	return f.events, f.err
}

func TestUFCProvider_MapsESPNEvents(t *testing.T) {
	fake := &fakeESPNClient{
		events: []espn.Event{{ID: "1", Name: "UFC 1", ShortName: "U1", Date: "2025-01-15T20:00:00Z"}},
	}
	p := &ufcProvider{c: fake}

	got, err := p.FetchEventsRange(context.Background(), "20250101", "20250131")
	if err != nil {
		t.Fatalf("FetchEventsRange error: %v", err)
	}
	if fake.gotStart != "20250101" || fake.gotEnd != "20250131" {
		t.Fatalf("adapter passed wrong range: %s-%s", fake.gotStart, fake.gotEnd)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].ID != "1" || got[0].Name != "UFC 1" || got[0].ShortName != "U1" || got[0].Date != "2025-01-15T20:00:00Z" {
		t.Fatalf("unexpected mapped event: %+v", got[0])
	}
}

// fakeProvider is a minimal Provider for manager tests.
type fakeProvider struct{}

func (f *fakeProvider) FetchEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]Event, error) {
	return nil, nil
}

func TestManager_RegisterAndLookup(t *testing.T) {
	m := NewManager()
	p1 := &fakeProvider{}
	p2 := &fakeProvider{}
	m.Register("ufc", p1)
	m.Register("pfl", p2)

	if got, ok := m.Provider("ufc"); !ok || got != p1 {
		t.Fatalf("lookup ufc failed or wrong provider: ok=%v got=%p want=%p", ok, got, p1)
	}
	if got, ok := m.Provider("pfl"); !ok || got != p2 {
		t.Fatalf("lookup pfl failed or wrong provider: ok=%v got=%p want=%p", ok, got, p2)
	}
	if _, ok := m.Provider("one"); ok {
		t.Fatalf("expected missing provider for 'one'")
	}
}

func TestNewDefaultManager_RegistersUFC(t *testing.T) {
	m := NewDefaultManager(nil, "test-agent")
	if _, ok := m.Provider("ufc"); !ok {
		t.Fatalf("expected default manager to have 'ufc' provider registered")
	}
}
