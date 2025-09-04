package main

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	cfgpkg "github.com/zodakzach/fight-night-discord-bot/internal/config"
	discpkg "github.com/zodakzach/fight-night-discord-bot/internal/discord"
	"github.com/zodakzach/fight-night-discord-bot/internal/logx"
	"github.com/zodakzach/fight-night-discord-bot/internal/migrate"
	"github.com/zodakzach/fight-night-discord-bot/internal/sentryx"
	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

func main() {
	logx.Init("fight-night-bot")
	cfg := cfgpkg.Load()

	// Initialize Sentry (no-op if SENTRY_DSN is not set)
	if err := sentryx.InitFromEnv("fight-night-bot"); err != nil {
		logx.Warn("sentry init failed", "err", err)
	}
	// Capture unexpected panics and still crash
	defer sentryx.Recover()

	// Apply DB migrations at startup to keep schema up-to-date.
	if err := migrate.Run(cfg.StatePath); err != nil {
		logx.Fatal("migrate.run failed", "err", err, "db", cfg.StatePath)
	}

	st := state.Load(cfg.StatePath)

	dg, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		logx.Fatal("discord session init failed", "err", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds

	// Bind handlers BEFORE opening so we don't miss the initial Ready event.
	mgr := sources.NewDefaultManager(http.DefaultClient, cfg.UserAgent)
	discpkg.BindHandlers(dg, st, cfg, mgr)

	logx.Info("opening discord gateway")
	if err := dg.Open(); err != nil {
		logx.Fatal("discord gateway open failed", "err", err)
	}
	defer dg.Close()
	logx.Info("discord gateway opened")

	discpkg.StartNotifier(dg, st, cfg, mgr)

	// Graceful shutdown on SIGINT/SIGTERM so Discord session closes cleanly.
	logx.Info("bot running; waiting for shutdown signal")
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	logx.Info("shutdown signal received; closing session")
	// Ensure any buffered Sentry events are sent before exit
	sentryx.Flush(2 * time.Second)
}
