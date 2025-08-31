package sources

import (
	"context"
	"testing"
)

// fakeProvider is a minimal Provider for manager tests.
type fakeProvider struct{}

func (f *fakeProvider) NextEvent(ctx context.Context) (string, string, bool, error) {
	return "", "", false, nil
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
