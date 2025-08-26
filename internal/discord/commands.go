package discord

import (
    "fmt"
    "log"
    "time"

    "github.com/bwmarrin/discordgo"

    "github.com/zodakzach/fight-night-discord-bot/internal/config"
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
                        Type:         discordgo.ApplicationCommandOptionChannel,
                        Name:         "channel",
                        Description:  "Channel to use (default: this channel)",
                        Required:     false,
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

func BindHandlers(s *discordgo.Session, st *state.Store, cfg config.Config) {
    s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
        log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
    })
    s.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
        handleInteraction(s, ic, st, cfg)
    })
}

func handleInteraction(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config) {
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
    _ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
        Type: discordgo.InteractionResponseChannelMessageWithSource,
        Data: &discordgo.InteractionResponseData{
            Content: content,
            Flags:   discordgo.MessageFlagsEphemeral,
        },
    })
}

 
