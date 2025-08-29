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

// fakeProvider implements sources.Provider for tests
type fakeProvider struct {
	byDate map[string][]sources.Event
	err    error
}

func (f *fakeProvider) FetchEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]sources.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.byDate == nil {
		return nil, nil
	}
	parse := func(s string) time.Time {
		t, _ := time.Parse("20060102", s)
		return t
	}
	start := parse(startYYYYMMDD)
	end := parse(endYYYYMMDD)
	var out []sources.Event
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		k := d.Format("20060102")
		if evs, ok := f.byDate[k]; ok {
			out = append(out, evs...)
		}
	}
	return out, nil
}

func TestHandleStatus_UsesDefaultTZWhenUnset(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	st := state.Load(":memory:")
	cfg := config.Config{TZ: "America/New_York", RunAt: "16:00"}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	handleStatus(s, ic, st, cfg)

	if !strings.Contains(got, "Timezone: "+cfg.TZ) {
		t.Fatalf("expected default TZ in reply, got: %q", got)
	}
	if !strings.Contains(got, "Run time: "+cfg.RunAt) {
		t.Fatalf("expected run time in reply, got: %q", got)
	}
}

func TestHandleStatus_UsesGuildTZWhenSet(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	st := state.Load(":memory:")
	st.UpdateGuildTZ("g1", "Europe/London")
	cfg := config.Config{TZ: "America/New_York", RunAt: "16:00"}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	handleStatus(s, ic, st, cfg)

	if !strings.Contains(got, "Timezone: Europe/London") {
		t.Fatalf("expected guild TZ in reply, got: %q", got)
	}
}

func TestHandleNextEvent_FindsUpcoming(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	st := state.Load(":memory:")
	st.UpdateGuildTZ("g1", "America/New_York")
	cfg := config.Config{TZ: "America/New_York"}

	// Prepare a fake event for tomorrow at noon UTC
	now := time.Now().UTC()
	tomorrow := now.Add(24 * time.Hour)
	dateKey := tomorrow.Format("20060102")
	f := &fakeProvider{byDate: map[string][]sources.Event{
		dateKey: {
			{ID: "1", Name: "UFC Fight Night: Test", ShortName: "UFC Test", Date: tomorrow.Format(time.RFC3339)},
		},
	}}
	mgr := sources.NewManager()
	mgr.Register("ufc", f)

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	oldDefer := deferInteractionResponse
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	defer func() { editInteractionResponse = old }()
	defer func() { deferInteractionResponse = oldDefer }()

	handleNextEvent(s, ic, st, cfg, mgr)

	if !strings.Contains(got, "Next UFC event: UFC Fight Night: Test") {
		t.Fatalf("expected next event in reply, got: %q", got)
	}
}

func TestHandleNextEvent_NoneFound(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	st := state.Load(":memory:")
	cfg := config.Config{TZ: "America/New_York"}

	f := &fakeProvider{byDate: map[string][]sources.Event{}}
	mgr := sources.NewManager()
	mgr.Register("ufc", f)

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	oldDefer := deferInteractionResponse
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	defer func() { editInteractionResponse = old }()
	defer func() { deferInteractionResponse = oldDefer }()

	handleNextEvent(s, ic, st, cfg, mgr)

	if !strings.Contains(got, "No upcoming UFC events") {
		t.Fatalf("expected no-events message, got: %q", got)
	}
}

func TestHandleHelp_IncludesKeyLines(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	handleHelp(s, ic)

	for _, want := range []string{"/set-org", "/set-channel", "/notify", "/set-tz", "/status", "/next-event"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help reply missing %q in %q", want, got)
		}
	}
}

func TestHandleSetTZ_UsageAndInvalidAndValid(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{StatePath: "", TZ: "America/New_York"}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	// No options -> usage
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data:    discordgo.ApplicationCommandInteractionData{Name: "set-tz"},
	}}
	handleSetTZ(s, ic, st, cfg)
	if !strings.Contains(got, "Usage: /set-tz") {
		t.Fatalf("expected usage when missing option, got %q", got)
	}

	// Invalid TZ
	got = ""
	ic = &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "set-tz",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Type:  discordgo.ApplicationCommandOptionString,
				Name:  "tz",
				Value: "Not/A_Real_TZ",
			}},
		},
	}}
	handleSetTZ(s, ic, st, cfg)
	if !strings.Contains(got, "Invalid timezone") {
		t.Fatalf("expected invalid tz message, got %q", got)
	}

	// Valid TZ updates state and replies accordingly
	got = ""
	ic = &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "set-tz",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Type:  discordgo.ApplicationCommandOptionString,
				Name:  "tz",
				Value: "Europe/London",
			}},
		},
	}}
	handleSetTZ(s, ic, st, cfg)
	if !strings.Contains(got, "Timezone updated to Europe/London") {
		t.Fatalf("expected success tz message, got %q", got)
	}
}

func TestHandleNotifyToggle_UsageWhenMissingOption(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data:    discordgo.ApplicationCommandInteractionData{Name: "notify"},
	}}
	handleNotifyToggle(s, ic, st, cfg)
	if !strings.Contains(got, "Usage: /notify state:<on|off>") {
		t.Fatalf("expected notify usage message, got %q", got)
	}
}

func TestHandleSetOrg_UsageWhenMissingOption(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{}

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data:    discordgo.ApplicationCommandInteractionData{Name: "set-org"},
	}}
	handleSetOrg(s, ic, st, cfg)
	if !strings.Contains(got, "Usage: /set-org org:<ufc>") {
		t.Fatalf("expected set-org usage message, got %q", got)
	}
}

func TestHandleNextEvent_ProviderErrorAndUnsupportedOrg(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{TZ: "America/New_York"}

	// Provider error
	mgr := sources.NewManager()
	mgr.Register("ufc", &fakeProvider{err: assertErr{}})

	var got string
	oldEdit := editInteractionResponse
	oldDefer := deferInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	defer func() { editInteractionResponse = oldEdit }()
	defer func() { deferInteractionResponse = oldDefer }()

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	handleNextEvent(s, ic, st, cfg, mgr)
	if !strings.Contains(got, "Error fetching events") {
		t.Fatalf("expected provider error message, got %q", got)
	}

	// Unsupported org
	got = ""
	st.UpdateGuildOrg("g1", "pride")
	mgr2 := sources.NewManager() // no provider registered for pride
	handleNextEvent(s, ic, st, cfg, mgr2)
	if !strings.Contains(got, "Unsupported organization") {
		t.Fatalf("expected unsupported org message, got %q", got)
	}
}

// assertErr is a simple error type for forced errors
type assertErr struct{}

func (assertErr) Error() string { return "assert error" }

func TestHandleNextEvent_TodayStartedBranch(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	st.UpdateGuildTZ("g1", "America/New_York")
	cfg := config.Config{TZ: "America/New_York"}

	now := time.Now().UTC()
	oneHourAgo := now.Add(-1 * time.Hour)
	dateKey := oneHourAgo.In(time.FixedZone("UTC", 0)).Format("20060102")

	f := &fakeProvider{byDate: map[string][]sources.Event{
		dateKey: {{ID: "1", Name: "UFC Fight Night: Ago", Date: oneHourAgo.Format(time.RFC3339)}},
	}}
	mgr := sources.NewManager()
	mgr.Register("ufc", f)

	var got string
	oldEdit := editInteractionResponse
	oldDefer := deferInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	defer func() { editInteractionResponse = oldEdit }()
	defer func() { deferInteractionResponse = oldDefer }()

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	handleNextEvent(s, ic, st, cfg, mgr)

	if !strings.Contains(got, "Today’s UFC event: UFC Fight Night: Ago") && !strings.Contains(got, "Today's UFC event: UFC Fight Night: Ago") {
		t.Fatalf("expected today's event message, got %q", got)
	}
	if !strings.Contains(got, "ago") {
		t.Fatalf("expected relative 'ago' timing, got %q", got)
	}
}

func TestParseAPITime_SupportedAndError(t *testing.T) {
	// concrete times matching supported layouts
	inputs := []string{
		"2024-08-27T12:34+00:00",
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339Nano),
		"2024-08-27T12:34:56+0000",
	}

	for _, in := range inputs {
		if _, err := parseAPITime(in); err != nil {
			t.Fatalf("expected to parse %q, got err %v", in, err)
		}
	}

	if _, err := parseAPITime("not-a-time"); err == nil {
		t.Fatalf("expected error for invalid time input")
	}
}

func TestHandleInteraction_GuardCases(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{TZ: "America/New_York"}
	mgr := sources.NewManager()

	var got string
	old := sendInteractionResponse
	sendInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { sendInteractionResponse = old }()

	// DM usage (no guild). Must include ApplicationCommand data to avoid discordgo panic.
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{Name: "help"},
	}}
	handleInteraction(s, ic, st, cfg, mgr)
	if !strings.Contains(got, "Please use this command in a server.") {
		t.Fatalf("expected DM guard message, got %q", got)
	}

	// Unknown command
	got = ""
	ic = &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		GuildID: "g1",
		Type:    discordgo.InteractionApplicationCommand,
		Data:    discordgo.ApplicationCommandInteractionData{Name: "unknown-cmd"},
	}}
	handleInteraction(s, ic, st, cfg, mgr)
	if !strings.Contains(got, "Unknown command.") {
		t.Fatalf("expected unknown command reply, got %q", got)
	}
}
