package discord

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
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
