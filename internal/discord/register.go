package discord

import (
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

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

	// Dev-only parent command with subcommands
	devTest := &discordgo.ApplicationCommand{
		Name:        "dev-test",
		Description: "[dev] Tools for testing",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "create-event",
				Description: "Create a scheduled event for the next org event",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "create-announcement",
				Description: "Post the next event message+embed now",
			},
		},
	}

	appID := s.State.User.ID
	// Log the intent to register commands with context
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	if devGuild != "" {
		// Include the dev-only command only for the dev guild registration.
		cmdsWithDev := make([]*discordgo.ApplicationCommand, 0, len(cmds)+1)
		cmdsWithDev = append(cmdsWithDev, cmds...)
		cmdsWithDev = append(cmdsWithDev, devTest)
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
