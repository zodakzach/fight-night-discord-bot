package discord

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func RegisterCommands(s *discordgo.Session, devGuild string) {
	cmd := &discordgo.ApplicationCommand{
		Name:        "notify",
		Description: "Configure UFC fight-night notifications",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-channel",
				Description: "Set the channel to post announcements",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: "Channel to use (default: this channel)",
						Required:    false,
						// GuildNews corresponds to announcement channels for this discordgo version
						ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews},
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "set-tz",
				Description: "Set the server timezone (IANA, e.g. America/New_York)",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "tz",
						Description: "Timezone name",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show current config",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "next",
				Description: "Show the next UFC event",
			},
		},
	}

	var err error
	if devGuild != "" {
		_, err = s.ApplicationCommandCreate(s.State.User.ID, devGuild, cmd)
	} else {
		_, err = s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
	}
	if err != nil {
		log.Printf("register commands: %v", err)
	}
}

func BindHandlers(s *discordgo.Session, st *state.Store, cfg config.Config, client espn.Client) {
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
	})
	s.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		handleInteraction(s, ic, st, cfg, client)
	})
}

func handleInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, client espn.Client) {
	if ic.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := ic.ApplicationCommandData()
	if data.Name != "notify" {
		return
	}
	if ic.GuildID == "" {
		replyEphemeral(s, ic, "Please use this command in a server.")
		return
	}

	sub := data.Options[0].Name
	switch sub {
	case "set-channel":
		handleSetChannel(s, ic, st, cfg)
	case "set-tz":
		handleSetTZ(s, ic, st, cfg)
	case "status":
		handleStatus(s, ic, st, cfg)
	case "next":
		handleNextEvent(s, ic, st, cfg, client)
	default:
		replyEphemeral(s, ic, "Unknown subcommand.")
	}
}

func handleSetChannel(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
	// Choose provided channel or current channel
	opts := ic.ApplicationCommandData().Options[0].Options
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

func handleSetTZ(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
	tz := ic.ApplicationCommandData().Options[0].Options[0].StringValue()
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
	replyEphemeral(s, ic, fmt.Sprintf("Channel: %s\nTimezone: %s\nRun time: %s", ch, tz, cfg.RunAt))
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

func handleNextEvent(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, client espn.Client) {
	// Acknowledge quickly to avoid the 3s interaction timeout.
	_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	// Timezone selection for display
	_, tzName, _ := st.GetGuildSettings(ic.GuildID)
	if tzName == "" {
		tzName = cfg.TZ
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.Local
	}

	nowUTC := time.Now().UTC()
	nowLocal := time.Now().In(loc)
	start := nowLocal.Format("20060102")
	end := nowLocal.AddDate(0, 0, 30).Format("20060102")

	events, err := client.FetchUFCEventsRange(context.Background(), start, end)
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
		_ = editInteractionResponse(s, ic, "No upcoming UFC events found in the next 30 days.")
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
		msg = fmt.Sprintf("Next UFC event: %s\nWhen: %s (%s) — in %s", nextName, localTime.Format("Mon Jan 2, 3:04 PM MST"), tzName, rel)
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
		msg = fmt.Sprintf("Today’s UFC event: %s\nStarted: %s (%s) — %s", nextName, localTime.Format("3:04 PM"), tzName, rel)
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
