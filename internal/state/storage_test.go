package state

import (
	"reflect"
	"sort"
	"testing"
)

func TestDefaults_WhenUnset(t *testing.T) {
	st := Load(":memory:")

	if got := st.GetGuildNotifyEnabled("g1"); got {
		t.Fatalf("expected notify default off, got on")
	}

	if has := st.HasGuildOrg("g1"); has {
		t.Fatalf("expected no explicit org set")
	}

	if org := st.GetGuildOrg("g1"); org != "ufc" {
		t.Fatalf("expected default org 'ufc', got %q", org)
	}

	ch, tz, last := st.GetGuildSettings("g1")
	if ch != "" || tz != "" {
		t.Fatalf("expected empty channel/tz, got ch=%q tz=%q", ch, tz)
	}
	if len(last) != 0 {
		t.Fatalf("expected empty last-posted map, got %v", last)
	}
}

func TestUpdateGuildSettings_PersistAndNoClobber(t *testing.T) {
	st := Load(":memory:")

	st.UpdateGuildChannel("g1", "c1")
	st.UpdateGuildTZ("g1", "America/New_York")
	st.UpdateGuildOrg("g1", "ufc")
	st.UpdateGuildNotifyEnabled("g1", true)

	ch, tz, _ := st.GetGuildSettings("g1")
	if ch != "c1" || tz != "America/New_York" {
		t.Fatalf("unexpected settings: ch=%q tz=%q", ch, tz)
	}

	if !st.GetGuildNotifyEnabled("g1") {
		t.Fatalf("expected notify on after enabling")
	}
	st.UpdateGuildNotifyEnabled("g1", false)
	if st.GetGuildNotifyEnabled("g1") {
		t.Fatalf("expected notify off after disabling")
	}

	if !st.HasGuildOrg("g1") {
		t.Fatalf("expected org to be explicitly set")
	}
	if org := st.GetGuildOrg("g1"); org != "ufc" {
		t.Fatalf("unexpected org: %q", org)
	}

	// No clobber: update TZ again and ensure channel unchanged
	st.UpdateGuildTZ("g1", "Europe/London")
	ch2, tz2, _ := st.GetGuildSettings("g1")
	if ch2 != "c1" || tz2 != "Europe/London" {
		t.Fatalf("expected channel preserved and tz updated, got ch=%q tz=%q", ch2, tz2)
	}
}

func TestGuildIDs_ReturnsPersistedGuilds(t *testing.T) {
	st := Load(":memory:")
	st.UpdateGuildChannel("g1", "c1")
	st.UpdateGuildChannel("g2", "c2")
	ids := st.GuildIDs()
	sort.Strings(ids)
	want := []string{"g1", "g2"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("guild ids: got %v, want %v", ids, want)
	}
}

func TestMarkPosted_UpsertAndRead(t *testing.T) {
	st := Load(":memory:")
	st.UpdateGuildChannel("g1", "c1") // ensure row

	st.MarkPosted("g1", "ufc", "2024-08-27")
	_, _, last := st.GetGuildSettings("g1")
	if got := last["ufc"]; got != "2024-08-27" {
		t.Fatalf("last-posted after first mark: got %q", got)
	}

	// Update date for same sport
	st.MarkPosted("g1", "ufc", "2024-09-01")
	_, _, last2 := st.GetGuildSettings("g1")
	if got := last2["ufc"]; got != "2024-09-01" {
		t.Fatalf("last-posted after update: got %q", got)
	}
}
