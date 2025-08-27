package discord

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

// fakeProvider returns fixed events for tests.
type fakeProv struct{ events []sources.Event }

func (f *fakeProv) FetchEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]sources.Event, error) {
	return f.events, nil
}

func TestParseHHMM(t *testing.T) {
	h, m, err := parseHHMM("16:00")
	if err != nil || h != 16 || m != 0 {
		t.Fatalf("parse valid: got h=%d m=%d err=%v", h, m, err)
	}
	if _, _, err := parseHHMM("24:00"); err == nil {
		t.Fatalf("expected error for invalid hour")
	}
	if _, _, err := parseHHMM("12:60"); err == nil {
		t.Fatalf("expected error for invalid minute")
	}
	if _, _, err := parseHHMM("nope"); err == nil {
		t.Fatalf("expected error for invalid format")
	}
}

func TestBuildMessage_FormatsHeaderAndLines(t *testing.T) {
	loc := time.UTC
	evs := []sources.Event{
		{Name: "Event A", Date: "2025-01-02T15:04:00Z"},
		{ShortName: "Event B", Date: "2025-01-02T18:30:00Z"},
	}
	msg := buildMessage("ufc", evs, loc)
	if !strings.HasPrefix(msg, "UFC Fight Night Alert:\n") {
		t.Fatalf("missing/incorrect header: %q", msg)
	}
	if !strings.Contains(msg, "• Event A — Thu 3:04 PM") {
		t.Fatalf("missing first line with time, got: %q", msg)
	}
	if !strings.Contains(msg, "• Event B — Thu 6:30 PM") {
		t.Fatalf("missing second line with time, got: %q", msg)
	}
	if !strings.Contains(msg, "there's a UFC event") {
		t.Fatalf("missing trailer with org, got: %q", msg)
	}
}

func TestNotifyGuild_SendsAndMarksPosted(t *testing.T) {
	// Prepare store and settings
	st := state.Load(":memory:")
	gid := "g1"
	st.UpdateGuildChannel(gid, "chan1")
	st.UpdateGuildTZ(gid, "UTC")
	st.UpdateGuildOrg(gid, "ufc")
	st.UpdateGuildNotifyEnabled(gid, true)

	// Event today in UTC
	now := time.Now().UTC()
	todayKey := now.Format("2006-01-02")
	ev := sources.Event{Name: "Test Event", Date: now.Format(time.RFC3339)}

	// Provider manager
	mgr := sources.NewManager()
	mgr.Register("ufc", &fakeProv{events: []sources.Event{ev}})

	// Capture outbound message
	sent := 0
	var lastMsg string
	old := sendChannelMessage
	sendChannelMessage = func(_ *discordgo.Session, _ string, content string) (*discordgo.Message, error) {
		sent++
		lastMsg = content
		return &discordgo.Message{Content: content}, nil
	}
	defer func() { sendChannelMessage = old }()

	// Run
	s := &discordgo.Session{}
	cfg := config.Config{TZ: "UTC"}
	notifyGuild(s, st, gid, mgr, cfg)

	if sent != 1 || !strings.Contains(lastMsg, "UFC Fight Night Alert:") || !strings.Contains(lastMsg, "Test Event") {
		t.Fatalf("expected one send with content, got sent=%d msg=%q", sent, lastMsg)
	}
	// Marked posted for org
	_, _, last := st.GetGuildSettings(gid)
	if last["ufc"] != todayKey {
		t.Fatalf("expected last-posted updated to %s, got %q", todayKey, last["ufc"])
	}

	// Second call should not send again
	notifyGuild(s, st, gid, mgr, cfg)
	if sent != 1 {
		t.Fatalf("expected no second send, got sent=%d", sent)
	}
}

func TestNotifyGuild_SkipsWhenNoOrgOrDisabled(t *testing.T) {
	st := state.Load(":memory:")
	gid := "g2"
	st.UpdateGuildChannel(gid, "chan1")
	// No org set, notify off by default

	mgr := sources.NewManager()
	mgr.Register("ufc", &fakeProv{events: nil})

	sent := 0
	old := sendChannelMessage
	sendChannelMessage = func(_ *discordgo.Session, _ string, content string) (*discordgo.Message, error) {
		sent++
		return &discordgo.Message{}, nil
	}
	defer func() { sendChannelMessage = old }()

	s := &discordgo.Session{}
	cfg := config.Config{TZ: "UTC"}
	notifyGuild(s, st, gid, mgr, cfg)

	if sent != 0 {
		t.Fatalf("expected no send when org unset and notify disabled, got %d", sent)
	}

	// Enable notify but still no org set -> still skip
	st.UpdateGuildNotifyEnabled(gid, true)
	notifyGuild(s, st, gid, mgr, cfg)
	if sent != 0 {
		t.Fatalf("expected no send when org unset even if notify enabled, got %d", sent)
	}
}
