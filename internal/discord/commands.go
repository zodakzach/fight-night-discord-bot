package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func handleInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	if ic.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := ic.ApplicationCommandData()
	if ic.GuildID == "" {
		replyEphemeral(s, ic, "Please use this command in a server.")
		return
	}

	// Trace which command was invoked and by whom
	userID := ""
	if ic.Member != nil && ic.Member.User != nil {
		userID = ic.Member.User.ID
	}
	logx.Debug("slash command invoked", "name", data.Name, "guild_id", ic.GuildID, "channel_id", ic.ChannelID, "user_id", userID)

	if !dispatchCommand(s, ic, st, cfg, mgr) {
		replyEphemeral(s, ic, "Unknown command.")
	}
}

func handleSetChannel(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	// Choose provided channel or current channel
	opts := ic.ApplicationCommandData().Options
	channelID := ic.ChannelID
	if len(opts) > 0 {
		channelID = opts[0].ChannelValue(s).ID
	}

	// Permission check: require Manage Channels or Admin on target channel
	if !requireManageOrAdmin(s, ic, channelID, "You need Manage Channels permission to set the announcement channel.") {
		return
	}

	st.UpdateGuildChannel(ic.GuildID, channelID)

	replyEphemeral(s, ic, "Notification channel updated.")
}

func handleNotifyToggle(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /notify state:<on|off>")
		return
	}
	state := opts[0].StringValue()

	// Permission check similar to set-channel
	if !requireManageOrAdmin(s, ic, ic.ChannelID, "You need Manage Channels permission to change notifications.") {
		return
	}

	switch state {
	case "on":
		if !st.HasGuildOrg(ic.GuildID) {
			replyEphemeral(s, ic, "Please set an organization first with /set-org before enabling notifications.")
			return
		}
		st.UpdateGuildNotifyEnabled(ic.GuildID, true)
		replyEphemeral(s, ic, "Notifications enabled.")
	case "off":
		st.UpdateGuildNotifyEnabled(ic.GuildID, false)
		replyEphemeral(s, ic, "Notifications disabled.")
	default:
		replyEphemeral(s, ic, "Invalid state. Use on or off.")
	}
}

func handleEventsToggle(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /events state:<on|off>")
		return
	}
	state := opts[0].StringValue()

	// Permission check similar to set-channel
	if !requireManageOrAdmin(s, ic, ic.ChannelID, "You need Manage Channels permission to change scheduled events.") {
		return
	}

	switch state {
	case "on":
		if !st.HasGuildOrg(ic.GuildID) {
			replyEphemeral(s, ic, "Please set an organization first with /set-org before enabling scheduled events.")
			return
		}
		st.UpdateGuildEventsEnabled(ic.GuildID, true)
		replyEphemeral(s, ic, "Scheduled events enabled (will create day-before).")
	case "off":
		st.UpdateGuildEventsEnabled(ic.GuildID, false)
		replyEphemeral(s, ic, "Scheduled events disabled.")
	default:
		replyEphemeral(s, ic, "Invalid state. Use on or off.")
	}
}

func handleSetOrg(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /set-org org:<ufc>")
		return
	}
	org := opts[0].StringValue()

	// Permission check similar to set-channel
	if !requireManageOrAdmin(s, ic, ic.ChannelID, "You need Manage Channels permission to set the organization.") {
		return
	}

	switch org {
	case "ufc":
		st.UpdateGuildOrg(ic.GuildID, org)
		replyEphemeral(s, ic, "Organization set to UFC.")
	default:
		replyEphemeral(s, ic, "Unsupported org. Currently only 'ufc' is available.")
	}
}

func handleOrgSettings(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	data := ic.ApplicationCommandData()
	if len(data.Options) == 0 {
		replyEphemeral(s, ic, "Usage: /org-settings ufc contender state:<ignore|include>")
		return
	}

	// Permission check similar to set-channel
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to change org settings.")
		return
	}

	group := data.Options[0]
	if group.Name == "ufc" {
		if len(group.Options) == 0 {
			replyEphemeral(s, ic, "Usage: /org-settings ufc contender-ignore|contender-include")
			return
		}
		sub := group.Options[0]
		switch sub.Name {
		case "contender-ignore":
			st.UpdateGuildUFCIgnoreContender(ic.GuildID, true)
			replyEphemeral(s, ic, "UFC Contender Series will be ignored.")
		case "contender-include":
			st.UpdateGuildUFCIgnoreContender(ic.GuildID, false)
			replyEphemeral(s, ic, "UFC Contender Series will be included.")
		default:
			replyEphemeral(s, ic, "Unknown UFC setting.")
		}
		return
	}

	replyEphemeral(s, ic, "Unknown org. Currently supported: ufc")
}

func handleSetTZ(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /set-tz tz:<IANA timezone>")
		return
	}
	tz := opts[0].StringValue()
	if _, err := time.LoadLocation(tz); err != nil {
		replyEphemeral(s, ic, "Invalid timezone. Example: America/Los_Angeles")
		return
	}
	st.UpdateGuildTZ(ic.GuildID, tz)
	replyEphemeral(s, ic, "Timezone updated to "+tz)
}

// handleCreateEvent: dev-only helper to create a scheduled event for the next org event.
func handleCreateEvent(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	// Basic checks
	if ic.GuildID == "" {
		replyEphemeral(s, ic, "Use in a server")
		return
	}
	if !st.HasGuildOrg(ic.GuildID) {
		replyEphemeral(s, ic, "Set an organization first with /set-org")
		return
	}
	// Permission: require Manage Events for invoker to reduce abuse during testing
	if ic.Member == nil || (ic.Member.Permissions&discordgo.PermissionManageEvents) == 0 {
		replyEphemeral(s, ic, "You need Manage Events to use this (dev).")
		return
	}

	// Resolve org (default to ufc) and provider
	org, provider, ctx, ok := providerForGuild(st, mgr, ic.GuildID, true)
	if !ok {
		replyEphemeral(s, ic, "Unsupported org provider")
		return
	}

	// Timezone selection for display and date filtering
	loc, _ := guildLocation(st, cfg, ic.GuildID)

	// Use provider to select next/ongoing event in guild TZ
	evt, ok, err := pickNextEvent(ctx, provider)
	if err != nil {
		replyEphemeral(s, ic, "Error fetching events: "+err.Error())
		return
	}
	if !ok {
		replyEphemeral(s, ic, "No upcoming event to create.")
		return
	}

	// Prevent duplicates: check by the event's local date
	stUTC, err := parseAPITime(evt.Start)
	if err != nil {
		replyEphemeral(s, ic, "Error parsing event time.")
		return
	}
	pickAt := stUTC.In(loc)
	evDateKey := pickAt.In(loc).Format("2006-01-02")
	if st.HasScheduledEvent(ic.GuildID, org, evDateKey) {
		replyEphemeral(s, ic, "An event already exists for "+evDateKey+".")
		return
	}

	startAt := pickAt
	endAt := startAt.Add(3 * time.Hour)
	params := &discordgo.GuildScheduledEventParams{
		Name:               strings.ToUpper(org) + ": " + evt.Name,
		Description:        "Created by dev command",
		ScheduledStartTime: &startAt,
		ScheduledEndTime:   &endAt,
		PrivacyLevel:       discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
		EntityType:         discordgo.GuildScheduledEventEntityTypeExternal,
		EntityMetadata:     &discordgo.GuildScheduledEventEntityMetadata{Location: "TBD"},
	}
	ev, err := s.GuildScheduledEventCreate(ic.GuildID, params)
	if err != nil {
		replyEphemeral(s, ic, "Create failed: "+err.Error())
		return
	}
	// Track by local date key to avoid duplicate creates
	st.MarkScheduledEvent(ic.GuildID, org, evDateKey, ev.ID)
	replyEphemeral(s, ic, "Scheduled event created: "+ev.Name)
}

// handleCreateAnnouncement: dev-only helper to post the next event's notifier message/embed immediately.
func handleCreateAnnouncement(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	// Basic checks
	if ic.GuildID == "" {
		replyEphemeral(s, ic, "Use in a server")
		return
	}
	if !st.HasGuildOrg(ic.GuildID) {
		replyEphemeral(s, ic, "Set an organization first with /set-org")
		return
	}

	// Choose target channel: prefer configured channel, else current channel
	chID := ic.ChannelID
	if ch, _, _ := st.GetGuildSettings(ic.GuildID); strings.TrimSpace(ch) != "" {
		chID = ch
	}

	// Permission: require Manage Channels or Admin in the target channel to reduce abuse
	if !requireManageOrAdmin(s, ic, chID, "You need Manage Channels permission to use this (dev).") {
		return
	}

	// Use the notifier code path with force=true to ensure it posts even when not event day.
	posted, reason := notifyGuildCore(s, st, ic.GuildID, mgr, cfg, true, chID)
	if posted {
		replyEphemeral(s, ic, "Announcement posted to <#"+chID+">")
		return
	}
	replyEphemeral(s, ic, "Skipped: "+reason)
}

func handleSetDelivery(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /set-delivery mode:<message|announcement>")
		return
	}
	mode := strings.ToLower(opts[0].StringValue())

	// Permission check similar to set-channel
	if !requireManageOrAdmin(s, ic, ic.ChannelID, "You need Manage Channels permission to change delivery mode.") {
		return
	}

	switch mode {
	case "message":
		st.UpdateGuildAnnounceEnabled(ic.GuildID, false)
		replyEphemeral(s, ic, "Delivery mode set to regular messages.")
	case "announcement":
		st.UpdateGuildAnnounceEnabled(ic.GuildID, true)
		replyEphemeral(s, ic, "Delivery mode set to announcements (when channel supports it).")
	default:
		replyEphemeral(s, ic, "Invalid mode. Use message or announcement.")
	}
}

func handleSetRunHour(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /set-run-hour hour:<0-23>")
		return
	}
	hour := int(opts[0].IntValue())
	if hour < 0 || hour > 23 {
		replyEphemeral(s, ic, "Invalid hour. Use 0-23 (e.g., 16)")
		return
	}

	// Permission check similar to set-channel
	if !requireManageOrAdmin(s, ic, ic.ChannelID, "You need Manage Channels permission to set the run hour.") {
		return
	}

	st.UpdateGuildRunHour(ic.GuildID, hour)
	replyEphemeral(s, ic, fmt.Sprintf("Daily run hour updated to %02d:00 (guild timezone)", hour))
}

func handleStatus(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
	ch, tz, _ := st.GetGuildSettings(ic.GuildID)
	if ch == "" {
		ch = "(not set)"
	}
	if tz == "" {
		tz = cfg.TZ
	}
	orgDisplay := "(not set)"
	if st.HasGuildOrg(ic.GuildID) {
		orgDisplay = strings.ToUpper(st.GetGuildOrg(ic.GuildID))
	}
	notify := "off"
	if st.GetGuildNotifyEnabled(ic.GuildID) {
		notify = "on"
	}
	events := "off"
	if st.GetGuildEventsEnabled(ic.GuildID) {
		events = "on"
	}
	delivery := "message"
	if st.GetGuildAnnounceEnabled(ic.GuildID) {
		delivery = "announcement"
	}
	runAt := cfg.RunAt
	if h := st.GetGuildRunHour(ic.GuildID); h >= 0 {
		runAt = fmt.Sprintf("%02d:00", h)
	}
	msg := fmt.Sprintf(
		"Channel: %s\nTimezone: %s\nOrg: %s\nNotifications: %s\nEvents: %s\nDelivery: %s\nRun time: %s",
		ch, tz, orgDisplay, notify, events, delivery, runAt,
	)
	// Append UFC-specific status when applicable
	if strings.EqualFold(orgDisplay, "UFC") || st.GetGuildOrg(ic.GuildID) == "ufc" {
		if st.GetGuildUFCIgnoreContender(ic.GuildID) {
			msg += "\nUFC Contender Series: ignored"
		} else {
			msg += "\nUFC Contender Series: included"
		}
	}
	replyEphemeral(s, ic, msg)
}

func handleHelp(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	replyEphemeral(s, ic, buildHelp())
}

func handleNextEvent(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	// Acknowledge quickly to avoid the 3s interaction timeout.
	_ = deferInteractionResponse(s, ic)

	// Timezone selection for display
	loc, tzName := guildLocation(st, cfg, ic.GuildID)

	// Resolve org+provider (default to UFC if unset) and build context
	org, provider, ctx, ok := providerForGuild(st, mgr, ic.GuildID, true)
	if !ok {
		_ = editInteractionResponse(s, ic, "Unsupported organization for next-event. Try /set-org to a supported one.")
		return
	}
	ev, ok, err := pickNextEvent(ctx, provider)
	if err != nil {
		_ = editInteractionResponse(s, ic, "Error fetching events. Please try again later.")
		return
	}
	if !ok {
		_ = editInteractionResponse(s, ic, "No upcoming "+strings.ToUpper(org)+" events found in the next 30 days.")
		return
	}
	// Parse event start for display
	startUTC, err := parseAPITime(ev.Start)
	if err != nil {
		_ = editInteractionResponse(s, ic, "Error parsing event time.")
		return
	}
	localTime := startUTC.In(loc)
	until := time.Until(startUTC).Truncate(time.Minute)
	msg := ""
	if until >= 0 {
		d := int(until.Hours()) / 24
		h := int(until.Hours()) % 24
		m := int(until.Minutes()) % 60
		rel := ""
		if d > 0 {
			rel = fmt.Sprintf("%dd %dh %dm", d, h, m)
		} else if h > 0 {
			rel = fmt.Sprintf("%dh %dm", h, m)
		} else {
			rel = fmt.Sprintf("%dm", m)
		}
		msg = fmt.Sprintf("Next %s event: %s\nWhen: %s (%s) — in %s", strings.ToUpper(org), ev.Name, localTime.Format("Mon Jan 2, 3:04 PM MST"), tzName, rel)
	} else {
		ago := -until
		h := int(ago.Hours())
		m := int(ago.Minutes()) % 60
		rel := ""
		if h > 0 {
			rel = fmt.Sprintf("%dh %dm ago", h, m)
		} else {
			rel = fmt.Sprintf("%dm ago", m)
		}
		msg = fmt.Sprintf("Today’s %s event: %s\nStarted: %s (%s) — %s", strings.ToUpper(org), ev.Name, localTime.Format("3:04 PM"), tzName, rel)
	}
	_ = editInteractionResponse(s, ic, msg)

	// Attempt to add a rich embed with card details (best-effort; ignore errors)
	if emb := buildEventEmbed(strings.ToUpper(org), tzName, loc, ev); emb != nil {
		_ = editInteractionEmbeds(s, ic, []*discordgo.MessageEmbed{emb})
	}
}
