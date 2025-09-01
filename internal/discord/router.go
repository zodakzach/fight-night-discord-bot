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
	"set-channel": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleSetChannel(s, ic, st)
	},
	"set-delivery": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleSetDelivery(s, ic, st)
	},
	"notify": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleNotifyToggle(s, ic, st)
	},
	"events": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleEventsToggle(s, ic, st)
	},
	"set-tz": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleSetTZ(s, ic, st)
	},
	"set-run-hour": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleSetRunHour(s, ic, st)
	},
	"set-org": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, _ config.Config, _ *sources.Manager) {
		handleSetOrg(s, ic, st)
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
	// Dev helpers
	"create-event": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
		handleCreateEvent(s, ic, st, cfg, mgr)
	},
	"create-announcement": func(s *discordgo.Session, ic *discordgo.InteractionCreate, st *state.Store, cfg config.Config, mgr *sources.Manager) {
		handleCreateAnnouncement(s, ic, st, cfg, mgr)
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
