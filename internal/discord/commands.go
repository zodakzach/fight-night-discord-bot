package discord

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func RegisterCommands(s *discordgo.Session, devGuild string) {
	// Define top-level commands matching README
	cmds := []*discordgo.ApplicationCommand{
		{
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
		{
			Name:        "set-org",
			Description: "Choose the organization (currently UFC only)",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "org",
					Description: "Organization",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "ufc", Value: "ufc"},
					},
				},
			},
		},
		{
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
		{
			Name:        "status",
			Description: "Show current bot settings for this guild",
		},
		{
			Name:        "help",
			Description: "Show available commands and usage",
		},
		{
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
		{
			Name:        "next-event",
			Description: "Show the next event for the selected org",
		},
	}

	appID := s.State.User.ID
	var (
		res []*discordgo.ApplicationCommand
		err error
	)
	if devGuild != "" {
		res, err = s.ApplicationCommandBulkOverwrite(appID, devGuild, cmds)
	} else {
		res, err = s.ApplicationCommandBulkOverwrite(appID, "", cmds)
	}
	if err != nil {
		log.Printf("bulk overwrite commands: %v", err)
		return
	}
	log.Printf("registered %d commands via bulk overwrite", len(res))
}

func BindHandlers(s *discordgo.Session, st *state.Store, cfg config.Config, mgr *sources.Manager) {
	var registerOnce sync.Once
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
		// Ensure commands are registered after Ready when application/user ID is available.
		registerOnce.Do(func() { RegisterCommands(s, cfg.DevGuild) })
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

	switch data.Name {
	case "set-channel":
		handleSetChannel(s, ic, st, cfg)
	case "notify":
		handleNotifyToggle(s, ic, st, cfg)
	case "set-tz":
		handleSetTZ(s, ic, st, cfg)
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

func handleSetChannel(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
	// Choose provided channel or current channel
	opts := ic.ApplicationCommandData().Options
	channelID := ic.ChannelID
	if len(opts) > 0 {
		channelID = opts[0].ChannelValue(s).ID
	}

	// Permission check: require Manage Channels or Admin on target channel
	perms, err := s.UserChannelPermissions(ic.Member.User.ID, channelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if perms&discordgo.PermissionManageChannels == 0 && perms&discordgo.PermissionAdministrator == 0 {
		replyEphemeral(s, ic, "You need Manage Channels permission to set the announcement channel.")
		return
	}

	st.UpdateGuildChannel(ic.GuildID, channelID)
	_ = st.Save(cfg.StatePath)

	replyEphemeral(s, ic, "Announcement channel updated.")
}

func handleNotifyToggle(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
	opts := ic.ApplicationCommandData().Options
	if len(opts) == 0 {
		replyEphemeral(s, ic, "Usage: /notify state:<on|off>")
		return
	}
	state := opts[0].StringValue()

	// Permission check similar to set-channel
	perms, err := s.UserChannelPermissions(ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if perms&discordgo.PermissionManageChannels == 0 && perms&discordgo.PermissionAdministrator == 0 {
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
		_ = st.Save(cfg.StatePath)
		replyEphemeral(s, ic, "Notifications enabled.")
	case "off":
		st.UpdateGuildNotifyEnabled(ic.GuildID, false)
		_ = st.Save(cfg.StatePath)
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
	perms, err := s.UserChannelPermissions(ic.Member.User.ID, ic.ChannelID)
	if err != nil {
		replyEphemeral(s, ic, "Could not check permissions.")
		return
	}
	if perms&discordgo.PermissionManageChannels == 0 && perms&discordgo.PermissionAdministrator == 0 {
		replyEphemeral(s, ic, "You need Manage Channels permission to set the organization.")
		return
	}

	switch org {
	case "ufc":
		st.UpdateGuildOrg(ic.GuildID, org)
		_ = st.Save(cfg.StatePath)
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
	_ = st.Save(cfg.StatePath)
	replyEphemeral(s, ic, "Timezone updated to "+tz)
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
	msg := fmt.Sprintf(
		"Channel: %s\nTimezone: %s\nOrg: %s\nNotifications: %s\nRun time: %s",
		ch, tz, orgDisplay, notify, cfg.RunAt,
	)
	replyEphemeral(s, ic, msg)
}

func handleHelp(s *discordgo.Session, ic *discordgo.InteractionCreate) {
	msg := "Commands:\n" +
		"- /set-org org:<ufc> — Pick your org. Required before enabling notifications.\n" +
		"- /set-channel [channel:#channel] — Choose post channel.\n" +
		"- /notify state:<on|off> — Toggle notifications (requires org set).\n" +
		"- /set-tz tz:<Region/City> — Set timezone (IANA).\n" +
		"- /status — Show current settings.\n" +
		"- /next-event — Show the next event for your org."
	replyEphemeral(s, ic, msg)
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
	_, tzName, _ := st.GetGuildSettings(ic.GuildID)
	if tzName == "" {
		tzName = cfg.TZ
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.Local
	}

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
	start := nowLocal.Format("20060102")
	end := nowLocal.AddDate(0, 0, 30).Format("20060102")

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

func parseAPITime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04Z07:00",   // no seconds (your sample)
		time.RFC3339,               // with seconds
		time.RFC3339Nano,           // with fractional seconds
		"2006-01-02T15:04:05Z0700", // no colon in offset
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", s)
}
