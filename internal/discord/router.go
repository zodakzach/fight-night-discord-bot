package discord

import (
	"github.com/bwmarrin/discordgo"
	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

// handlerFunc is a unified signature for routing slash commands.
type handlerFunc func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager)

// routes maps command names to handlers. Thin wrappers adapt to existing handler signatures.
var routes = map[string]handlerFunc{
	"settings": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
		handleSettings(s, ic, st, cfg, mgr)
	},
	"org-settings": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleOrgSettings(s, ic, st)
	},
	"status": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, _ *sources.Manager) {
		handleStatus(s, ic, st, cfg)
	},
	"help": func(s *discordgo.Session, ic *discordgo.InteractionCreate, _ *state.Store, _ config.Config, _ *sources.Manager) {
		handleHelp(s, ic)
	},
	"next-event": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
		handleNextEvent(s, ic, st, cfg, mgr)
	},
	// Dev helpers grouped under /dev-test
	"dev-test": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
		handleDevTest(s, ic, st, cfg, mgr)
	},
}

// dispatchCommand runs a mapped handler if present and returns whether it handled.
func dispatchCommand(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) bool {
	name := ic.ApplicationCommandData().Name
	if h, ok := routes[name]; ok {
		h(s, ic, st, cfg, mgr)
		return true
	}
	return false
}
