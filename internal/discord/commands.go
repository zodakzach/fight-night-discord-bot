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
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "state",
						Description: "Enable or disable notifications",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "on", Value: "on"},
							{Name: "off", Value: "off"},
						},
					},
				},
			},
			Note: "Requires org to be set (use /set-org)",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-org",
				Description: "Choose the organization (currently UFC only)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "org",
						Description: "Organization",
						Required:    true,
						Choices:     orgChoices,
					},
				},
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-tz",
				Description: "Set the guild's timezone (IANA name)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "tz",
						Description: "Timezone, e.g., America/Los_Angeles",
						Required:    true,
					},
				},
			},
			Note: "Example: America/Los_Angeles",
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-run-hour",
				Description: "Set daily notification hour (0-23)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "hour",
						Description: "Hour of day (0-23)",
						Required:    true,
					},
				},
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
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:         discordgo.ApplicationCommandOptionChannel,
						Name:         "channel",
						Description:  "Channel to use (default: this channel)",
						Required:     false,
						ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews},
					},
				},
			},
		},
		{
			Def: &discordgo.ApplicationCommand{
				Name:        "set-delivery",
				Description: "Choose message delivery: regular message or announcement",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "mode",
						Description: "Delivery mode",
						Required:    true,
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "message", Value: "message"},
							{Name: "announcement", Value: "announcement"},
						},
					},
				},
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

	appID := s.State.User.ID
	// Log the intent to register commands with context
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	if devGuild != "" {
		logx.Info("registering slash commands", "target", "guild", "app_id", appID, "guild_id", devGuild, "count", len(cmds), "names", names)
		res, err := s.ApplicationCommandBulkOverwrite(appID, devGuild, cmds)
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
		handleNotifyToggle(s, ic, st, cfg)
	case "set-tz":
		handleSetTZ(s, ic, st, cfg)
	case "set-run-hour":
		handleSetRunHour(s, ic, st, cfg)
	case "set-org":
		handleSetOrg(s, ic, st, cfg)
	case "status":
		handleStatus(s, ic, st, cfg)
	case "help":
		handleHelp(s, ic)
	case "next-event":
		handleNextEvent(s, ic, st, cfg, mgr)
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

func handleNotifyToggle(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
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

func handleSetOrg(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
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

func handleSetTZ(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
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

func handleSetRunHour(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
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
	delivery := "message"
	if st.GetGuildAnnounceEnabled(ic.GuildID) {
		delivery = "announcement"
	}
	runAt := cfg.RunAt
	if h := st.GetGuildRunHour(ic.GuildID); h >= 0 {
		runAt = fmt.Sprintf("%02d:00", h)
	}
	msg := fmt.Sprintf(
		"Channel: %s\nTimezone: %s\nOrg: %s\nNotifications: %s\nDelivery: %s\nRun time: %s",
		ch, tz, orgDisplay, notify, delivery, runAt,
	)
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

	// Resolve org/provider
	org := st.GetGuildOrg(ic.GuildID)
	if org == "" {
		org = "ufc"
	}
	provider, ok := mgr.Provider(org)
	if !ok {
		_ = editInteractionResponse(s, ic, "Unsupported organization for next-event. Try /set-org to a supported one.")
		return
	}

	nowUTC := time.Now().UTC()
	nowLocal := time.Now().In(loc)
	// Query provider by UTC date keys and include yesterday to cover
	// events that started late last night UTC but are "today" locally.
	start := nowUTC.AddDate(0, 0, -1).Format("20060102")
	end := nowUTC.AddDate(0, 0, 30).Format("20060102")

	events, err := provider.FetchEventsRange(context.Background(), start, end)
	if err != nil {
		_ = editInteractionResponse(s, ic, "Error fetching events. Please try again later.")
		return
	}

	todayKey := nowLocal.Format("20060102")

	var todayName string
	var todayAt time.Time
	var futureName string
	var futureAt time.Time
	// Track the most recent already-started event in case there is
	// nothing else today and no upcoming future within the window.
	var recentName string
	var recentAt time.Time

	for _, e := range events {
		t, err := parseAPITime(e.Date /* or e.StartDate if that’s the field */)
		if err != nil {
			continue
		}
		evLocalKey := t.In(loc).Format("20060102")
		name := e.Name
		if name == "" {
			name = e.ShortName
		}
		if evLocalKey == todayKey {
			if todayAt.IsZero() || t.Before(todayAt) {
				todayAt, todayName = t, name
			}
			continue
		}
		if t.After(nowUTC) {
			if futureAt.IsZero() || t.Before(futureAt) {
				futureAt, futureName = t, name
			}
			continue
		}
		// Track the most recent past event as a fallback (e.g., started recently)
		if recentAt.IsZero() || t.After(recentAt) {
			recentAt, recentName = t, name
		}
	}

	var nextName string
	var nextAt time.Time
	if !todayAt.IsZero() {
		nextAt, nextName = todayAt, todayName
	} else if !futureAt.IsZero() {
		nextAt, nextName = futureAt, futureName
	}

	if nextAt.IsZero() {
		// If nothing today or in the future, prefer a recently-started
		// event within the last 12 hours so users still get a useful answer.
		if !recentAt.IsZero() && nowUTC.Sub(recentAt) <= 12*time.Hour {
			nextAt, nextName = recentAt, recentName
		}
	}
	if nextAt.IsZero() {
		_ = editInteractionResponse(s, ic, "No upcoming "+strings.ToUpper(org)+" events found in the next 30 days.")
		return
	}

	localTime := nextAt.In(loc)
	until := time.Until(nextAt).Truncate(time.Minute)
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
		msg = fmt.Sprintf("Next %s event: %s\nWhen: %s (%s) — in %s", strings.ToUpper(org), nextName, localTime.Format("Mon Jan 2, 3:04 PM MST"), tzName, rel)
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
		msg = fmt.Sprintf("Today’s %s event: %s\nStarted: %s (%s) — %s", strings.ToUpper(org), nextName, localTime.Format("3:04 PM"), tzName, rel)
	}
	_ = editInteractionResponse(s, ic, msg)
}
