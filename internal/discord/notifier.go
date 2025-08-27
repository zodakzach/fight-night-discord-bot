package discord

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func StartNotifier(s *discordgo.Session, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	loc, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		log.Printf("Invalid TZ %q; using local: %v", cfg.TZ, err)
		loc = time.Local
	}
	go func() {
		time.Sleep(2 * time.Second)
		runNotifierOnce(s, st, mgr, cfg)
		scheduleDaily(cfg.RunAt, loc, func() { runNotifierOnce(s, st, mgr, cfg) })
	}()
}

func scheduleDaily(hhmm string, loc *time.Location, fn func()) {
	h, m, err := parseHHMM(hhmm)
	if err != nil {
		log.Printf("Invalid RUN_AT %q: %v; using 16:00", hhmm, err)
		h, m = 16, 0
	}
	for {
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		delay := time.Until(next)
		log.Printf("Next check at %s (%s from now)", next.Format(time.RFC1123), delay.Truncate(time.Second))
		timer := time.NewTimer(delay)
		<-timer.C
		fn()
	}
}

func runNotifierOnce(s *discordgo.Session, st *state.Store, mgr *sources.Manager, cfg config.Config) {
	for _, gid := range st.GuildIDs() {
		notifyGuild(s, st, gid, mgr, cfg)
	}
}

func notifyGuild(s *discordgo.Session, st *state.Store, guildID string, mgr *sources.Manager, cfg config.Config) {
	channelID, tzName, lastPosted := st.GetGuildSettings(guildID)
	if channelID == "" {
		return
	}

	// Respect per-guild notify enabled flag (default enabled when unset)
	if !st.GetGuildNotifyEnabled(guildID) {
		return
	}

	// Require org to be explicitly set and have a provider
	if !st.HasGuildOrg(guildID) {
		return
	}
	org := st.GetGuildOrg(guildID)
	provider, ok := mgr.Provider(org)
	if !ok {
		log.Printf("Guild %s: no provider for org %q", guildID, org)
		return
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil || tzName == "" {
		loc, _ = time.LoadLocation(cfg.TZ)
	}
	now := time.Now().In(loc)
	todayYYYYMMDD := now.Format("20060102")
	todayKey := now.Format("2006-01-02")

	// Fetch events for today and filter to those occurring today in the guild timezone.
	events, err := provider.FetchEventsRange(context.Background(), todayYYYYMMDD, todayYYYYMMDD)
	if err != nil {
		log.Printf("Guild %s: fetch error: %v", guildID, err)
		return
	}
	var todays []sources.Event
	for _, e := range events {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		if t.In(loc).Format("20060102") == todayYYYYMMDD {
			todays = append(todays, e)
		}
	}
	if len(todays) == 0 {
		return
	}

	already := lastPosted != nil && lastPosted[org] == todayKey
	if already {
		return
	}

	msg := buildMessage(org, todays, loc)
	if _, err := s.ChannelMessageSend(channelID, msg); err != nil {
		log.Printf("Guild %s: send message error: %v", guildID, err)
		return
	}

	st.MarkPosted(guildID, org, todayKey)
	_ = st.Save(cfg.StatePath)
}

func buildMessage(org string, events []sources.Event, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(strings.ToUpper(org) + " Fight Night Alert:\n")
	for _, e := range events {
		name := e.Name
		if name == "" {
			name = e.ShortName
		}
		tstr := ""
		if t, err := time.Parse(time.RFC3339, e.Date); err == nil {
			tstr = t.In(loc).Format("Mon 3:04 PM")
		}
		if tstr != "" {
			fmt.Fprintf(&b, "• %s — %s\n", name, tstr)
		} else {
			fmt.Fprintf(&b, "• %s\n", name)
		}
	}
	b.WriteString("\nI'll check daily and post here when there's a " + strings.ToUpper(org) + " event.")
	return b.String()
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return h, m, nil
}
