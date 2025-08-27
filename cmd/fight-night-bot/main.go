package main

import (
	"log"
	"net/http"

	"github.com/bwmarrin/discordgo"

	cfgpkg "github.com/zodakzach/fight-night-discord-bot/internal/config"
	discpkg "github.com/zodakzach/fight-night-discord-bot/internal/discord"
	"github.com/zodakzach/fight-night-discord-bot/internal/espn"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func main() {
	cfg := cfgpkg.Load()

	st := state.Load(cfg.StatePath)

	dg, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		log.Fatalf("discord session: %v", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds

	if err := dg.Open(); err != nil {
		log.Fatalf("open gateway: %v", err)
	}
	defer dg.Close()

	discpkg.RegisterCommands(dg, cfg.DevGuild)

	espnClient := espn.NewClient(http.DefaultClient, cfg.UserAgent)
	discpkg.BindHandlers(dg, st, cfg, espnClient)
	discpkg.StartNotifier(dg, st, cfg, espnClient)

	log.Println("Bot running. Press Ctrl+C to exit.")
	select {}
}
