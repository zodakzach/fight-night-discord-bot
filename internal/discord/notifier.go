package discord

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func StartNotifier(s *discordgo.Session, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	// Run on an hourly schedule and only notify guilds whose configured run hour
	// matches the current hour in their timezone. This supports per-guild overrides
	// while keeping the env RUN_AT as the default (minutes ignored).
	go func() {
		time.Sleep(2 * time.Second)
		runNotifierTick(s, st, mgr, cfg)
		scheduleHourly(func() { runNotifierTick(s, st, mgr, cfg) })
	}()
}

// runNotifierTick loops all guilds and notifies only those matching the configured run time.
func runNotifierTick(s *discordgo.Session, st *state.Store, mgr *sources.Manager, cfg config.Config) {
	now := time.Now()
	for _, gid := range st.GuildIDs() {
		if shouldRunNow(st, gid, cfg, now) {
			notifyGuild(s, st, gid, mgr, cfg)
		}
	}
}

// shouldRunNow returns true if the given moment's hour matches the guild's configured
// hour (guild override via state, falling back to cfg.RunAt) in the guild's timezone
// (falling back to cfg.TZ when unset/invalid).
func shouldRunNow(st *state.Store, guildID string, cfg config.Config, instant time.Time) bool {
	// Determine timezone
	_, tzName, _ := st.GetGuildSettings(guildID)
	if tzName == "" {
		tzName = cfg.TZ
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.Local
	}
	// Determine run hour
	hour := st.GetGuildRunHour(guildID)
	if hour < 0 {
		// Fall back to env default RUN_AT
		if hh, _, err := parseHHMM(cfg.RunAt); err == nil {
			hour = hh
		} else {
			// ultimate fallback
			hour, _ = strconv.Atoi(strings.Split(config.DefaultRunAt, ":")[0])
		}
	}
	tlocal := instant.In(loc)
	return tlocal.Hour() == hour
}

// scheduleHourly invokes fn at the start of each UTC hour (which aligns to :00 in all timezones).
func scheduleHourly(fn func()) {
	for {
		now := time.Now()
		next := now.Truncate(time.Hour).Add(time.Hour)
		delay := time.Until(next)
		timer := time.NewTimer(delay)
		<-timer.C
		fn()
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
		logx.Warn("no provider for org", "guild_id", guildID, "org", org)
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
		logx.Error("events fetch error", "guild_id", guildID, "err", err)
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
	if _, err := sendChannelMessage(s, channelID, msg); err != nil {
		logx.Error("send message error", "guild_id", guildID, "err", err)
		return
	}

	st.MarkPosted(guildID, org, todayKey)
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

// sendChannelMessage is an indirection for tests to capture outbound messages
// without performing real Discord API calls.
var sendChannelMessage = func(s *discordgo.Session, channelID, content string) (*discordgo.Message, error) {
	return s.ChannelMessageSend(channelID, content)
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
