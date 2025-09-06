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
	name string
	at   time.Time
	ok   bool
	err  error
}

func (f *fakeProvider) NextEvent(_ context.Context) (*sources.Event, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	if !f.ok {
		return nil, false, nil
	}
	return &sources.Event{Org: "ufc", Name: f.name, Start: f.at.UTC().Format(time.RFC3339)}, true, nil
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

	// Force next-event resolver to return a known event tomorrow
	now := time.Now().UTC()
	tomorrow := now.Add(24 * time.Hour)
	oldGet := getNextEventFunc
	getNextEventFunc = func(_ context.Context, _ sources.Provider) (*sources.Event, bool, error) {
		return &sources.Event{Org: "ufc", Name: "UFC Fight Night: Test", Start: tomorrow.UTC().Format(time.RFC3339)}, true, nil
	}
	defer func() { getNextEventFunc = oldGet }()
	mgr := sources.NewDefaultManager(nil, "test-agent")
	orgKey := "ufc"
	st.UpdateGuildOrg("g1", orgKey)
	if _, ok := mgr.Provider(orgKey); !ok {
		t.Fatalf("test setup: provider not registered for ufc")
	}
	mgr.Register("ufc", &fakeProvider{})
	st.UpdateGuildOrg("g1", "ufc")
	mgr.Register("ufc", &fakeProvider{})

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	oldDefer := deferInteractionResponse
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	// Avoid real HTTP calls for embed edit
	oldEmb := editInteractionEmbeds
	editInteractionEmbeds = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, _ []*discordgo.MessageEmbed) error {
		return nil
	}
	defer func() { editInteractionResponse = old }()
	defer func() { deferInteractionResponse = oldDefer }()
	defer func() { editInteractionEmbeds = oldEmb }()

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

	// Force no upcoming event
	oldGet := getNextEventFunc
	getNextEventFunc = func(_ context.Context, _ sources.Provider) (*sources.Event, bool, error) {
		return nil, false, nil
	}
	defer func() { getNextEventFunc = oldGet }()
	mgr := sources.NewManager()
	mgr.Register("ufc", &fakeProvider{})

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	oldDefer := deferInteractionResponse
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	// Avoid real HTTP calls for embed edit
	oldEmb := editInteractionEmbeds
	editInteractionEmbeds = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, _ []*discordgo.MessageEmbed) error {
		return nil
	}
	defer func() { editInteractionResponse = old }()
	defer func() { deferInteractionResponse = oldDefer }()
	defer func() { editInteractionEmbeds = oldEmb }()

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

	for _, want := range []string{"/settings org", "/settings channel", "/settings notifications", "/settings timezone", "/status", "/next-event"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help reply missing %q in %q", want, got)
		}
	}
}

func TestHandleSetTZ_UsageAndInvalidAndValid(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")

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
	handleSetTZ(s, ic, st)
	if !strings.Contains(got, "Usage: /settings timezone") {
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
	handleSetTZ(s, ic, st)
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
	handleSetTZ(s, ic, st)
	if !strings.Contains(got, "Timezone updated to Europe/London") {
		t.Fatalf("expected success tz message, got %q", got)
	}
}

func TestHandleNotifyToggle_UsageWhenMissingOption(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")

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
	handleNotifyToggle(s, ic, st)
	if !strings.Contains(got, "Usage: /settings notifications state:<on|off>") {
		t.Fatalf("expected notify usage message, got %q", got)
	}
}

func TestHandleSetOrg_UsageWhenMissingOption(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")

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
	handleSetOrg(s, ic, st)
	if !strings.Contains(got, "Usage: /settings org org:<ufc>") {
		t.Fatalf("expected set-org usage message, got %q", got)
	}
}

func TestHandleNextEvent_ProviderErrorAndUnsupportedOrg(t *testing.T) {
	s := &discordgo.Session{}
	st := state.Load(":memory:")
	cfg := config.Config{TZ: "America/New_York"}

	// Simulate fetch error via next-event path
	mgr := sources.NewManager()
	mgr.Register("ufc", &fakeProvider{})
	oldGet := getNextEventFunc
	getNextEventFunc = func(_ context.Context, _ sources.Provider) (*sources.Event, bool, error) {
		return nil, false, assertErr{}
	}

	var got string
	oldEdit := editInteractionResponse
	oldDefer := deferInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	deferInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate) error { return nil }
	// Avoid real HTTP calls for embed edit
	oldEmb := editInteractionEmbeds
	editInteractionEmbeds = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, _ []*discordgo.MessageEmbed) error {
		return nil
	}
	defer func() { editInteractionResponse = oldEdit }()
	defer func() { deferInteractionResponse = oldDefer }()
	defer func() { editInteractionEmbeds = oldEmb }()

	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	handleNextEvent(s, ic, st, cfg, mgr)
	if !strings.Contains(got, "Error fetching events") {
		t.Fatalf("expected provider error message, got %q", got)
	}

	// Unsupported org (no provider registered)
	got = ""
	st.UpdateGuildOrg("g1", "pride")
	mgr2 := sources.NewManager() // no provider registered for pride
	handleNextEvent(s, ic, st, cfg, mgr2)
	if !strings.Contains(got, "Unsupported organization") {
		t.Fatalf("expected unsupported org message, got %q", got)
	}
	// restore
	getNextEventFunc = oldGet
}

// assertErr is a simple error type for forced errors
type assertErr struct{}

func (assertErr) Error() string { return "assert error" }

// Note: The 'today started' branch is covered implicitly by pickNextEvent response
// shape and message builder. Command path has tests for success and error paths.

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
