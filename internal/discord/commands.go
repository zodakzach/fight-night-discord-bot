package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

// commandSpec holds the source-of-truth for a command definition and any extra
// notes used for help text. We derive Discord registration and help content
// from these specs to avoid duplication.
type commandSpec struct {
	Def  *discordgo.ApplicationCommand
	Note string // Optional extra usage/help note
}

// currentSpecs stores the active command specs built during registration.
var currentSpecs []commandSpec

// commandSpecs builds the list of commands the bot supports using the
// provided org choices for the /set-org command.
func commandSpecs(orgs []string) []commandSpec {
	// Build choices for orgs
	orgChoices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(orgs))
	for _, o := range orgs {
		orgChoices = append(orgChoices, &discordgo.ApplicationCommandOptionChoice{Name: o, Value: o})
	}
	return []commandSpec{
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "notify",
				Description: "Enable or disable fight-night posts for this guild",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "state",
					Description: "Enable or disable notifications",
					Required:    true,
					Choices:     []*discordgo.ApplicationCommandOptionChoice{{Name: "on", Value: "on"}, {Name: "off", Value: "off"}},
				}},
			},
			Note: "Requires org to be set (use /set-org)",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "events",
				Description: "Enable or disable creating Scheduled Events (day-before)",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "state",
					Description: "Enable or disable scheduled events",
					Required:    true,
					Choices:     []*discordgo.ApplicationCommandOptionChoice{{Name: "on", Value: "on"}, {Name: "off", Value: "off"}},
				}},
			},
			Note: "Creates a Discord Scheduled Event the day before fight night.",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-org",
				Description: "Choose the organization (currently UFC only)",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "org",
					Description: "Organization",
					Required:    true,
					Choices:     orgChoices,
				}},
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "org-settings",
				Description: "Org-specific settings (UFC, etc.)",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
					Name:        "ufc",
					Description: "UFC-specific settings",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "contender-ignore",
							Description: "Ignore UFC Contender Series events (default)",
						},
						{
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Name:        "contender-include",
							Description: "Include UFC Contender Series events",
						},
					},
				}},
			},
			Note: "Use: /org-settings ufc contender-ignore|contender-include",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-tz",
				Description: "Set the guild's timezone (IANA name)",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "tz",
					Description: "Timezone, e.g., America/Los_Angeles",
					Required:    true,
				}},
			},
			Note: "Example: America/Los_Angeles",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-run-hour",
				Description: "Set daily notification hour (0-23)",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "hour",
					Description: "Hour of day (0-23)",
					Required:    true,
				}},
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "status",
				Description: "Show current bot settings for this guild",
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "help",
				Description: "Show available commands and usage",
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-channel",
				Description: "Pick the channel for notifications",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:         discordgo.ApplicationCommandOptionChannel,
					Name:         "channel",
					Description:  "Channel to use (default: this channel)",
					Required:     false,
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews},
				}},
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-delivery",
				Description: "Choose message delivery: regular message or announcement",
				Options: []*discordgo.ApplicationCommandOption{{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "mode",
					Description: "Delivery mode",
					Required:    true,
					Choices:     []*discordgo.ApplicationCommandOptionChoice{{Name: "message", Value: "message"}, {Name: "announcement", Value: "announcement"}},
				}},
			},
			Note: "Announcement mode publishes in Announcement channels and may require Manage Messages.",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "next-event",
				Description: "Show the next event for the selected org",
			},
		},
	}
}

func getSpecs() []commandSpec {
	if currentSpecs == nil {
		currentSpecs = commandSpecs([]string{"ufc"})
	}
	return currentSpecs
}

// applicationCommands converts specs to a list of discord ApplicationCommand definitions.
func applicationCommands() []*discordgo.ApplicationCommand {
	list := getSpecs()
	out := make([]*discordgo.ApplicationCommand, 0, len(list))
	for _, s := range list {
		out = append(out, s.Def)
	}
	return out
}

// buildHelp returns a help message generated from specs, so it stays in sync
// with the registered slash commands. The help omits the "help" command itself.
func buildHelp() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, s := range getSpecs() {
		if s.Def.Name == "help" { // avoid listing help in help
			continue
		}
		usage := "/" + s.Def.Name
		if len(s.Def.Options) > 0 {
			parts := make([]string, 0, len(s.Def.Options))
			for _, opt := range s.Def.Options {
				seg := opt.Name + ":" + optionUsage(opt)
				if !opt.Required {
					seg = "[" + seg + "]"
				}
				parts = append(parts, seg)
			}
			usage += " " + strings.Join(parts, " ")
		}
		b.WriteString("- ")
		b.WriteString(usage)
		if desc := strings.TrimSpace(s.Def.Description); desc != "" {
			b.WriteString(" — ")
			b.WriteString(desc)
		}
		if note := strings.TrimSpace(s.Note); note != "" {
			b.WriteString(". ")
			b.WriteString(note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func optionUsage(opt *discordgo.ApplicationCommandOption) string {
	// If choices exist, render like <a|b|c>
	if len(opt.Choices) > 0 {
		names := make([]string, 0, len(opt.Choices))
		for _, c := range opt.Choices {
			names = append(names, fmt.Sprint(c.Name))
		}
		return "<" + strings.Join(names, "|") + ">"
	}
	switch opt.Type {
	case discordgo.ApplicationCommandOptionString:
		return "<string>"
	case discordgo.ApplicationCommandOptionInteger:
		return "<number>"
	case discordgo.ApplicationCommandOptionChannel:
		return "#channel"
	case discordgo.ApplicationCommandOptionBoolean:
		return "<true|false>"
	case discordgo.ApplicationCommandOptionUser:
		return "@user"
	default:
		return "<value>"
	}
}

func RegisterCommands(s *discordgo.Session, devGuild string, mgr *sources.Manager) {
	// Rebuild specs with dynamic org choices from the manager
	orgs := []string{"ufc"}
	if mgr != nil {
		if o := mgr.Orgs(); len(o) > 0 {
			orgs = o
		}
	}
	currentSpecs = commandSpecs(orgs)
	// Define top-level commands from centralized specs
	cmds := applicationCommands()

	// Dev-only helper commands
	devCreateEvent := &discordgo.ApplicationCommand{
		Name:        "create-event",
		Description: "[dev] Create a scheduled event for the next org event",
	}
	devCreateAnnouncement := &discordgo.ApplicationCommand{
		Name:        "create-announcement",
		Description: "[dev] Post the next event message+embed now",
	}

	appID := s.State.User.ID
	// Log the intent to register commands with context
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	if devGuild != "" {
		// Include the dev-only commands only for the dev guild registration.
		cmdsWithDev := make([]*discordgo.ApplicationCommand, 0, len(cmds)+2)
		cmdsWithDev = append(cmdsWithDev, cmds...)
		cmdsWithDev = append(cmdsWithDev, devCreateEvent, devCreateAnnouncement)
		logx.Info("registering slash commands", "target", "guild", "app_id", appID, "guild_id", devGuild, "count", len(cmds), "names", names)
		res, err := s.ApplicationCommandBulkOverwrite(appID, devGuild, cmdsWithDev)
		if err != nil {
			logx.Error("bulk overwrite commands", "err", err, "target", "guild", "app_id", appID, "guild_id", devGuild)
			return
		}
		registered := make([]string, 0, len(res))
		for _, c := range res {
			registered = append(registered, c.Name)
		}
		logx.Info("commands registered", "target", "guild", "count", len(res), "names", registered)

		// Clear global commands to avoid duplicates while developing with a dev guild.
		logx.Info("clearing global commands due to dev guild configuration", "app_id", appID)
		if _, err := s.ApplicationCommandBulkOverwrite(appID, "", []*discordgo.ApplicationCommand{}); err != nil {
			logx.Warn("failed clearing global commands", "err", err, "app_id", appID)
		} else {
			logx.Info("global commands cleared")
		}
		return
	}

	// No dev guild: register globally.
	logx.Info("registering slash commands", "target", "global", "app_id", appID, "count", len(cmds), "names", names)
	res, err := s.ApplicationCommandBulkOverwrite(appID, "", cmds)
	if err != nil {
		logx.Error("bulk overwrite commands", "err", err, "target", "global", "app_id", appID)
		return
	}
	registered := make([]string, 0, len(res))
	for _, c := range res {
		registered = append(registered, c.Name)
	}
	logx.Info("commands registered", "target", "global", "count", len(res), "names", registered)

	// Clear guild-scoped commands to avoid guild+global duplicates.
	if strings.TrimSpace(devGuild) != "" {
		logx.Info("clearing dev guild commands due to global registration", "app_id", appID, "guild_id", devGuild)
		if _, err := s.ApplicationCommandBulkOverwrite(appID, devGuild, []*discordgo.ApplicationCommand{}); err != nil {
			logx.Warn("failed clearing dev guild commands", "err", err, "app_id", appID, "guild_id", devGuild)
		} else {
			logx.Info("dev guild commands cleared", "guild_id", devGuild)
		}
	} else {
		// No dev guild configured; sweep all guilds to ensure no leftover guild-scoped
		// commands remain that would duplicate the newly-registered global commands.
		clearAllGuildCommands(s, appID)
	}
}

// clearAllGuildCommands clears guild-scoped application commands for all guilds
// in the current session state. Safe to call in prod after registering global commands.
func clearAllGuildCommands(s *discordgo.Session, appID string) {
	for _, g := range s.State.Guilds {
		gid := g.ID
		// Best-effort: list commands to log names; proceed even if list fails.
		names := []string{}
		if cmds, err := s.ApplicationCommands(appID, gid); err == nil {
			for _, c := range cmds {
				names = append(names, c.Name)
			}
		}
		logx.Info("clearing guild commands", "guild_id", gid, "names", names)
		if _, err := s.ApplicationCommandBulkOverwrite(appID, gid, []*discordgo.ApplicationCommand{}); err != nil {
			logx.Warn("failed clearing guild commands", "guild_id", gid, "err", err)
		} else {
			logx.Info("guild commands cleared", "guild_id", gid)
		}
	}
}

func BindHandlers(s *discordgo.Session, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	var registerOnce sync.Once
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		logx.Info("discord ready", "user", r.User.Username, "discriminator", r.User.Discriminator)
		// Ensure commands are registered after Ready when application/user ID is available.
		registerOnce.Do(func() { RegisterCommands(s, cfg.DevGuild, mgr) })
	})
	s.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		handleInteraction(s, ic, st, cfg, mgr)
	})
}

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

	switch data.Name {
	case "set-channel":
		handleSetChannel(s, ic, st)
	case "set-delivery":
		handleSetDelivery(s, ic, st)
	case "notify":
		handleNotifyToggle(s, ic, st)
	case "events":
		handleEventsToggle(s, ic, st)
	case "set-tz":
		handleSetTZ(s, ic, st)
	case "set-run-hour":
		handleSetRunHour(s, ic, st)
	case "set-org":
		handleSetOrg(s, ic, st)
	case "org-settings":
		handleOrgSettings(s, ic, st)
	case "status":
		handleStatus(s, ic, st, cfg)
	case "help":
		handleHelp(s, ic)
	case "next-event":
		handleNextEvent(s, ic, st, cfg, mgr)
	case "create-event":
		handleCreateEvent(s, ic, st, cfg, mgr)
	case "create-announcement":
		handleCreateAnnouncement(s, ic, st, cfg, mgr)
	default:
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, channelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to set the announcement channel.")
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to change notifications.")
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to change scheduled events.")
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to set the organization.")
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
	org := st.GetGuildOrg(ic.GuildID)
	if org == "" {
		org = "ufc"
	}
	provider, ok := mgr.Provider(org)
	if !ok {
		replyEphemeral(s, ic, "Unsupported org provider")
		return
	}

	// Timezone selection for display and date filtering
	loc, _ := guildLocation(st, cfg, ic.GuildID)

	// Use provider to select next/ongoing event in guild TZ
	ctx := context.Background()
	if org == "ufc" {
		ctx = sources.WithUFCIgnoreContender(ctx, st.GetGuildUFCIgnoreContender(ic.GuildID))
	}
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
	if ic.Member == nil || ic.Member.User == nil {
		replyEphemeral(s, ic, "Missing member context")
		return
	}
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, chID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to use this (dev).")
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to change delivery mode.")
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
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if !ok {
		replyEphemeral(s, ic, "You need Manage Channels permission to set the run hour.")
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

func replyEphemeral(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) {
	_ = sendInteractionResponse(s, ic, content)
}

// sendInteractionResponse is a small indirection to allow tests to capture responses
// without performing real HTTP requests via discordgo. Tests may override this var.
var sendInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) error {
	return s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// editInteractionResponse allows tests to capture the final content when using deferred responses.
var editInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) error {
	_, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{Content: &content})
	return err
}

// deferInteractionResponse allows tests to avoid making real HTTP requests when acknowledging.
var deferInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate) error {
	return s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

func handleNextEvent(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	// Acknowledge quickly to avoid the 3s interaction timeout.
	_ = deferInteractionResponse(s, ic)

	// Timezone selection for display
	loc, tzName := guildLocation(st, cfg, ic.GuildID)

	// Resolve org (for display) and ensure we have a provider registered for it
	org := st.GetGuildOrg(ic.GuildID)
	if org == "" {
		org = "ufc"
	}
	if _, ok := mgr.Provider(org); !ok {
		_ = editInteractionResponse(s, ic, "Unsupported organization for next-event. Try /set-org to a supported one.")
		return
	}

	provider, _ := mgr.Provider(org)
	// Build provider context with per-guild UFC options
	ctx := context.Background()
	if org == "ufc" {
		ctx = sources.WithUFCIgnoreContender(ctx, st.GetGuildUFCIgnoreContender(ic.GuildID))
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
