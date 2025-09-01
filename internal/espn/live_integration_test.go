package espn

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
)

// requireLive consults config for whether live ESPN tests are enabled.
func requireLive(t *testing.T) {
	t.Helper()
	if config.LiveESPNEnabled() {
		return
	}
	t.Skip("skipping live ESPN integration test; enable via config.LiveESPNEnabled (set ESPN_LIVE=1)")
}

// TestLive_ESPN_ScoreboardAndCard fetches the current year's scoreboard from ESPN's
// public API and logs a sample of the payload. It then attempts to fetch the card
// for the first event (if present) and logs a few bouts.
func TestLive_ESPN_ScoreboardAndCard(t *testing.T) {
	requireLive(t)

	c := NewClient(nil, "fight-night-bot-test")
	year := fmt.Sprintf("%d", time.Now().UTC().Year())

	root, err := c.FetchUFCScoreboardRoot(context.Background(), year)
	if err != nil {
		t.Fatalf("FetchUFCScoreboardRoot(%s) error: %v", year, err)
	}
	t.Logf("Scoreboard %s: leagues=%d events=%d", year, len(root.Leagues), len(root.Events))

	if len(root.Events) == 0 {
		t.Log("No events returned in scoreboard for current year.")
		return
	}

	ev := root.Events[0]
	t.Logf("First event: ID=%s Name=%q Date=%s ShortName=%q Competitions=%d",
		ev.ID, ev.Name, ev.Date, ev.ShortName, len(ev.Competitions))

	if ev.ID == "" {
		t.Log("First event has no ID; skipping card fetch.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bouts, err := c.FetchUFCCardForEvent(ctx, ev.ID)
	if err != nil {
		t.Logf("FetchUFCCardForEvent(%s) error: %v", ev.ID, err)
		return
	}

	t.Logf("Bouts for event %s: %d", ev.ID, len(bouts))
	max := len(bouts)
	if max > 8 {
		max = 8
	}
	for i := 0; i < max; i++ {
		b := bouts[i]
		t.Logf("  - [%s] %s vs %s", b.WeightClass, b.Fighter1, b.Fighter2)
	}
}

// TestLive_ESPN_NextOrOngoing picks the next or ongoing event (UTC) and logs
// the selected window and a few fights from the resolved card (if any).
func TestLive_ESPN_NextOrOngoing(t *testing.T) {
	requireLive(t)

	c := NewClient(nil, "fight-night-bot-test")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ev, fights, st, en, ok, err := c.FetchNextOrOngoingEventAndCard(ctx, nil, time.Now)
	if err != nil {
		t.Fatalf("FetchNextOrOngoingEventAndCard error: %v", err)
	}
	if !ok || ev == nil {
		t.Log("No next/ongoing event selected.")
		return
	}
	t.Logf("Selected event: ID=%s Name=%q Start(UTC)=%s End(UTC)=%s Fights=%d",
		ev.ID, ev.Name, st.UTC().Format(time.RFC3339), en.UTC().Format(time.RFC3339), len(fights))

	max := len(fights)
	if max > 8 {
		max = 8
	}
	for i := 0; i < max; i++ {
		f := fights[i]
		t.Logf("  - [%s] %s (%s) vs %s (%s) winner=%s at %s",
			f.WeightClass, f.RedName, f.RedRecord, f.BlueName, f.BlueRecord, f.Winner,
			f.Scheduled.UTC().Format(time.RFC3339))
	}
}
