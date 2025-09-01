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
	// Production path: no force, no channel override
	_, _ = notifyGuildCore(s, st, guildID, mgr, cfg, false, "")
}

// notifyGuildCore performs the same logic as notifyGuild, with extras to support
// dev/testing via a force flag and an optional channel override. It returns whether
// a message was posted and a human-readable reason when it didn’t.
func notifyGuildCore(s *discordgo.Session, st *state.Store, guildID string, mgr *sources.Manager, cfg config.Config, force bool, channelOverride string) (bool, string) {
	chConfigured, _, lastPosted := st.GetGuildSettings(guildID)
	channelID := strings.TrimSpace(channelOverride)
	if channelID == "" {
		channelID = chConfigured
	}
	if channelID == "" {
		return false, "No channel configured"
	}

	// Respect per-guild notify enabled flag unless forced
	if !force && !st.GetGuildNotifyEnabled(guildID) {
		return false, "Notifications disabled"
	}

	// Require org to be explicitly set (for display/reporting)
	if !st.HasGuildOrg(guildID) {
		return false, "Organization not set"
	}
	org := st.GetGuildOrg(guildID)
	// Provider is used for next-event selection
	provider, ok := mgr.Provider(org)
	if !ok {
		logx.Warn("no provider for org", "guild_id", guildID, "org", org)
		return false, "No provider for org"
	}

	loc, tz := guildLocation(st, cfg, guildID)
	now := time.Now().In(loc)

	// Use provider-driven selection and gate on "today" only unless forced.
	// Build provider context with per-guild UFC options
	ctx := context.Background()
	if org == "ufc" {
		ctx = sources.WithUFCIgnoreContender(ctx, st.GetGuildUFCIgnoreContender(guildID))
	}
	evt, okNext, err := pickNextEvent(ctx, provider)
	if err != nil || !okNext {
		return false, "No upcoming event"
	}
	stUTC, err := parseAPITime(evt.Start)
	if err != nil {
		return false, "Invalid event time"
	}
	nextAt := stUTC.In(loc)
	postDayYYYYMMDD := nextAt.In(loc).Format("20060102")
	if !force && now.Format("20060102") != postDayYYYYMMDD {
		// Not the event day; skip posting when not forced.
		return false, "Not event day"
	}
	todayKey := nextAt.In(loc).Format("2006-01-02")

	already := lastPosted != nil && lastPosted[org] == todayKey
	if !force && already {
		return false, "Already posted today"
	}
	// Build a lightweight one-event list from the selected pick for messaging.
	todays := []sources.Event{{
		Org:       org,
		Name:      evt.Name,
		ShortName: evt.ShortName,
		Start:     nextAt.UTC().Format(time.RFC3339),
	}}
	msg := buildMessage(org, todays, loc)
	// Build embed for the event details
	emb := buildEventEmbed(strings.ToUpper(org), tz, loc, evt)
	toSend := &discordgo.MessageSend{Content: msg}
	if emb != nil {
		toSend.Embeds = []*discordgo.MessageEmbed{emb}
	}
	sent, sendErr := sendChannelMessageComplex(s, channelID, toSend)
	if sendErr != nil {
		logx.Error("send message error", "guild_id", guildID, "err", sendErr)
		return false, "Send failed"
	}

	// If announcement mode is enabled and the channel supports it, attempt to crosspost.
	if st.GetGuildAnnounceEnabled(guildID) && sent != nil {
		ch, chErr := s.Channel(channelID)
		if chErr == nil && ch != nil && ch.Type == discordgo.ChannelTypeGuildNews {
			if _, xerr := s.ChannelMessageCrosspost(channelID, sent.ID); xerr != nil {
				logx.Warn("crosspost failed", "guild_id", guildID, "channel_id", channelID, "message_id", sent.ID, "err", xerr)
			}
		}
	}

	if !force {
		st.MarkPosted(guildID, org, todayKey)
	}
	return true, "OK"
}

// ensureTomorrowScheduledEvent creates a Discord Scheduled Event the day before the
// next event (based on guild timezone) if not already created.
func ensureTomorrowScheduledEvent(s *discordgo.Session, st *state.Store, guildID string, mgr *sources.Manager, cfg config.Config) {
	// Require org and events toggle enabled to avoid surprising behavior.
	if !st.GetGuildEventsEnabled(guildID) || !st.HasGuildOrg(guildID) {
		return
	}
	org := st.GetGuildOrg(guildID)
	loc, _ := guildLocation(st, cfg, guildID)
	nowLocal := time.Now().In(loc)
	provider, ok := mgr.Provider(org)
	if !ok {
		return
	}
	// We want to create the event exactly on the day before the event (at the guild's run hour).
	// So: find the next upcoming event, get its local date, and only create if today == eventDate - 1 day.

	// Use the same next-event selection logic as the command.
	// Build provider context with per-guild UFC options
	ctx := context.Background()
	if org == "ufc" {
		ctx = sources.WithUFCIgnoreContender(ctx, st.GetGuildUFCIgnoreContender(guildID))
	}
	evt, ok, err := pickNextEvent(ctx, provider)
	if err != nil || !ok {
		return
	}
	stUTC, err := parseAPITime(evt.Start)
	if err != nil {
		return
	}
	evLocal := stUTC.In(loc)
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
	start := stUTC.In(loc)
	end := start.Add(3 * time.Hour)
	// Manage Events permission is required for the bot; if missing, this will fail.
	params := &discordgo.GuildScheduledEventParams{
		Name:               strings.ToUpper(org) + ": " + evt.Name,
		Description:        "Auto-created by Fight Night bot",
		ScheduledStartTime: &start,
		ScheduledEndTime:   &end,
		PrivacyLevel:       discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
		EntityType:         discordgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata:     &discordgo.GuildScheduledEventEntityMetadata{Location: "TBD"},
	}
	sev, err := s.GuildScheduledEventCreate(guildID, params)
	if err != nil {
		logx.Warn("scheduled event create failed", "guild_id", guildID, "org", org, "err", err)
		return
	}
	// Mark by the actual event date to avoid duplicates for the same event
	st.MarkScheduledEvent(guildID, org, evDateKey, sev.ID)
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
		ts := e.Start
		if t, err := parseAPITime(ts); err == nil {
			tstr = t.In(loc).Format("Mon 3:04 PM")
		}
		if tstr != "" {
			fmt.Fprintf(&b, "• %s — %s\n", name, tstr)
		} else {
			fmt.Fprintf(&b, "• %s\n", name)
		}
	}
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
