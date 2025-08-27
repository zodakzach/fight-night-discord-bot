package discord

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

// fakeClient implements espn.Client for tests
type fakeClient struct {
	byDate map[string][]espn.Event
	err    error
}

func (f *fakeClient) FetchUFCEvents(ctx context.Context, yyyymmdd string) ([]espn.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.byDate == nil {
		return nil, nil
	}
	return f.byDate[yyyymmdd], nil
}

func (f *fakeClient) FetchUFCEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]espn.Event, error) {
	if f.err != nil {
		return nil, f.err
	}
	// naive range iteration day by day
	if f.byDate == nil {
		return nil, nil
	}
	parse := func(s string) time.Time {
		t, _ := time.Parse("20060102", s)
		return t
	}
	start := parse(startYYYYMMDD)
	end := parse(endYYYYMMDD)
	var out []espn.Event
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
	st := state.Load("does-not-exist.json")
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
	st := state.Load("does-not-exist.json")
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
	st := state.Load("does-not-exist.json")
	st.UpdateGuildTZ("g1", "America/New_York")
	cfg := config.Config{TZ: "America/New_York"}

	// Prepare a fake event for tomorrow at noon UTC
	now := time.Now().UTC()
	tomorrow := now.Add(24 * time.Hour)
	dateKey := tomorrow.Format("20060102")
	f := &fakeClient{byDate: map[string][]espn.Event{
		dateKey: {
			{ID: "1", Name: "UFC Fight Night: Test", ShortName: "UFC Test", Date: tomorrow.Format(time.RFC3339)},
		},
	}}

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { editInteractionResponse = old }()

	handleNextEvent(s, ic, st, cfg, f)

	if !strings.Contains(got, "Next UFC event: UFC Fight Night: Test") {
		t.Fatalf("expected next event in reply, got: %q", got)
	}
}

func TestHandleNextEvent_NoneFound(t *testing.T) {
	s := &discordgo.Session{}
	ic := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{GuildID: "g1"}}
	st := state.Load("does-not-exist.json")
	cfg := config.Config{TZ: "America/New_York"}

	f := &fakeClient{byDate: map[string][]espn.Event{}}

	var got string
	old := editInteractionResponse
	editInteractionResponse = func(_ *discordgo.Session, _ *discordgo.InteractionCreate, content string) error {
		got = content
		return nil
	}
	defer func() { editInteractionResponse = old }()

	handleNextEvent(s, ic, st, cfg, f)

	if !strings.Contains(got, "No upcoming UFC events") {
		t.Fatalf("expected no-events message, got: %q", got)
	}
}
