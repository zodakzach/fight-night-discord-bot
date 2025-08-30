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
			// Create tomorrow's scheduled event first (if any), then post today's message.
			ensureTomorrowScheduledEvent(s, st, gid, mgr, cfg)
			notifyGuild(s, st, gid, mgr, cfg)
		}
	}
}

// shouldRunNow returns true if the given moment's hour matches the guild's configured
// hour (guild override via state, falling back to cfg.RunAt) in the guild's timezone
// (falling back to cfg.TZ when unset/invalid).
func shouldRunNow(st *state.Store, guildID string, cfg config.Config, instant time.Time) bool {
	// Determine timezone
	loc, _ := guildLocation(st, cfg, guildID)
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
	channelID, _, lastPosted := st.GetGuildSettings(guildID)
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

	loc, _ := guildLocation(st, cfg, guildID)
	now := time.Now().In(loc)

	// Use shared next-event selection and gate on "today" only.
	_, nextAt, ok, err := pickNextEvent(provider, loc)
	if err != nil || !ok {
		return
	}
	postDayYYYYMMDD := nextAt.In(loc).Format("20060102")
	if now.Format("20060102") != postDayYYYYMMDD {
		// Not the event day; skip posting.
		return
	}
	todayKey := nextAt.In(loc).Format("2006-01-02")

	already := lastPosted != nil && lastPosted[org] == todayKey
	if already {
		return
	}

	// Re-fetch only today’s events for list formatting
	events, err := provider.FetchEventsRange(context.Background(), postDayYYYYMMDD, postDayYYYYMMDD)
	if err != nil {
		logx.Error("events fetch error", "guild_id", guildID, "err", err)
		return
	}
	var todays []sources.Event
	for _, e := range events {
		t, err := parseAPITime(e.Date)
		if err != nil {
			continue
		}
		if t.In(loc).Format("20060102") == postDayYYYYMMDD {
			todays = append(todays, e)
		}
	}
	if len(todays) == 0 {
		return
	}

	msg := buildMessage(org, todays, loc)
	sent, err := sendChannelMessage(s, channelID, msg)
	if err != nil {
		logx.Error("send message error", "guild_id", guildID, "err", err)
		return
	}

	// If announcement mode is enabled and the channel supports it, attempt to crosspost.
	if st.GetGuildAnnounceEnabled(guildID) && sent != nil {
		ch, chErr := s.Channel(channelID)
		if chErr == nil && ch != nil && ch.Type == discordgo.ChannelTypeGuildNews {
			if _, xerr := s.ChannelMessageCrosspost(channelID, sent.ID); xerr != nil {
				logx.Warn("crosspost failed", "guild_id", guildID, "channel_id", channelID, "message_id", sent.ID, "err", xerr)
			}
		} else {
			// Not a news/announcement channel; skip crosspost silently.
		}
	}

	st.MarkPosted(guildID, org, todayKey)
}

// ensureTomorrowScheduledEvent creates a Discord Scheduled Event the day before the
// next event (based on guild timezone) if not already created.
func ensureTomorrowScheduledEvent(s *discordgo.Session, st *state.Store, guildID string, mgr *sources.Manager, cfg config.Config) {
	// Require org and events toggle enabled to avoid surprising behavior.
	if !st.GetGuildEventsEnabled(guildID) || !st.HasGuildOrg(guildID) {
		return
	}
	org := st.GetGuildOrg(guildID)
	provider, ok := mgr.Provider(org)
	if !ok {
		return
	}
	loc, _ := guildLocation(st, cfg, guildID)
	nowLocal := time.Now().In(loc)
	// We want to create the event exactly on the day before the event (at the guild's run hour).
	// So: find the next upcoming event, get its local date, and only create if today == eventDate - 1 day.

	// Use the same next-event selection logic as the command.
	pickName, pickAt, ok, err := pickNextEvent(provider, loc)
	if err != nil || !ok {
		return
	}

	evLocal := pickAt.In(loc)
	evDateKey := evLocal.Format("2006-01-02")
	// Only create on the day before the event
	if nowLocal.Format("2006-01-02") != evLocal.AddDate(0, 0, -1).Format("2006-01-02") {
		return
	}
	// Skip if already created for this event date
	if st.HasScheduledEvent(guildID, org, evDateKey) {
		return
	}

	// Create an EXTERNAL scheduled event at the event start time; end time = +3h.
	start := pickAt
	end := start.Add(3 * time.Hour)
	// Manage Events permission is required for the bot; if missing, this will fail.
	params := &discordgo.GuildScheduledEventParams{
		Name:               strings.ToUpper(org) + ": " + pickName,
		Description:        "Auto-created by Fight Night bot",
		ScheduledStartTime: &start,
		ScheduledEndTime:   &end,
		PrivacyLevel:       discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
		EntityType:         discordgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata:     &discordgo.GuildScheduledEventEntityMetadata{Location: "TBD"},
	}
	ev, err := s.GuildScheduledEventCreate(guildID, params)
	if err != nil {
		logx.Warn("scheduled event create failed", "guild_id", guildID, "org", org, "err", err)
		return
	}
	// Mark by the actual event date to avoid duplicates for the same event
	st.MarkScheduledEvent(guildID, org, evDateKey, ev.ID)
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
		if t, err := parseAPITime(e.Date); err == nil {
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
