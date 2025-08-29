package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"

	cfgpkg "github.com/zodakzach/fight-night-discord-bot/internal/config"
	discpkg "github.com/zodakzach/fight-night-discord-bot/internal/discord"
	"github.com/zodakzach/fight-night-discord-bot/internal/migrate"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func main() {
	cfg := cfgpkg.Load()

	// Apply DB migrations at startup to keep schema up-to-date.
	if err := migrate.Run(cfg.StatePath); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

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

	mgr := sources.NewDefaultManager(http.DefaultClient, cfg.UserAgent)
	discpkg.BindHandlers(dg, st, cfg, mgr)
	discpkg.StartNotifier(dg, st, cfg, mgr)

	// Graceful shutdown on SIGINT/SIGTERM so Discord session closes cleanly.
	log.Println("Bot running. Waiting for shutdown signal (Ctrl+C or SIGTERM)...")
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("Shutdown signal received; closing Discord session...")
}
