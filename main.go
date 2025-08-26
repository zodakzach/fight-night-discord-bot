package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	defaultTZ        = "America/New_York"
	defaultRunAt     = "16:00" // HH:MM process-local time for daily check
	defaultStateFile = "state.json"

    // ESPN scoreboard JSON endpoint; %s is YYYYMMDD
    ufcEventsUrl = "https://site.api.espn.com/apis/site/v2/sports/mma/ufc/scoreboard?dates=%s"
	userAgent = "ufc-fight-night-notifier/1.0 (contact: zach@codeezy.dev)"
)

var (
	runAt     = getEnv("RUN_AT", defaultRunAt)
	statePath = getEnv("STATE_FILE", defaultStateFile)
	globalTZ  = getEnv("TZ", defaultTZ)
	devGuild  = os.Getenv("GUILD_ID") // optional: register commands in one guild
)

type State struct {
	Guilds map[string]*GuildConfig `json:"guilds"`
	mu     sync.RWMutex            `json:"-"`
}

type GuildConfig struct {
	ChannelID  string            `json:"channel_id"`
	Timezone   string            `json:"timezone"`
	LastPosted map[string]string `json:"last_posted"` // sport -> YYYY-MM-DD
}

type Scoreboard struct {
	Events []ESPNEvent `json:"events"`
}
type ESPNEvent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	Date      string `json:"date"` // RFC3339
}

func main() {
	token := mustEnv("DISCORD_TOKEN")

	state := loadState(statePath)

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("discord session: %v", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds

	dg.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
	})
	dg.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		handleInteraction(s, ic, state)
	})

	if err := dg.Open(); err != nil {
		log.Fatalf("open gateway: %v", err)
	}
	defer dg.Close()

	registerCommands(dg)

	loc, err := time.LoadLocation(globalTZ)
	if err != nil {
		log.Printf("Invalid TZ %q; using local: %v", globalTZ, err)
		loc = time.Local
	}
	go func() {
		time.Sleep(2 * time.Second)
		runNotifierOnce(dg, state)
		scheduleDaily(runAt, loc, func() { runNotifierOnce(dg, state) })
	}()

	log.Println("Bot running. Press Ctrl+C to exit.")
	select {}
}

func registerCommands(s *discordgo.Session) {
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

func handleInteraction(
	s *discordgo.Session,
	ic *discordgo.InteractionCreate,
	state *State,
) {
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
		handleSetChannel(s, ic, state)
	case "set-tz":
		handleSetTZ(s, ic, state)
	case "status":
		handleStatus(s, ic, state)
	default:
		replyEphemeral(s, ic, "Unknown subcommand.")
	}
}

func handleSetChannel(
	s *discordgo.Session,
	ic *discordgo.InteractionCreate,
	state *State,
) {
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
	if perms&discordgo.PermissionManageChannels == 0 &&
		perms&discordgo.PermissionAdministrator == 0 {
		replyEphemeral(
			s,
			ic,
			"You need Manage Channels permission to set the announcement channel.",
		)
		return
	}

	g := ensureGuild(state, ic.GuildID)
	state.mu.Lock()
	g.ChannelID = channelID
	state.mu.Unlock()
	_ = saveState(statePath, state)

	replyEphemeral(s, ic, "Announcement channel updated.")
}

func handleSetTZ(
	s *discordgo.Session,
	ic *discordgo.InteractionCreate,
	state *State,
) {
	tz := ic.ApplicationCommandData().Options[0].Options[0].StringValue()
	if _, err := time.LoadLocation(tz); err != nil {
		replyEphemeral(s, ic, "Invalid timezone. Example: America/Los_Angeles")
		return
	}
	g := ensureGuild(state, ic.GuildID)
	state.mu.Lock()
	g.Timezone = tz
	state.mu.Unlock()
	_ = saveState(statePath, state)
	replyEphemeral(s, ic, "Timezone updated to "+tz)
}

func handleStatus(
	s *discordgo.Session,
	ic *discordgo.InteractionCreate,
	state *State,
) {
	g := ensureGuild(state, ic.GuildID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	channel := g.ChannelID
	if channel == "" {
		channel = "(not set)"
	}
	tz := g.Timezone
	if tz == "" {
		tz = globalTZ
	}
	replyEphemeral(
		s,
		ic,
		fmt.Sprintf("Channel: %s\nTimezone: %s\nRun time: %s", channel, tz, runAt),
	)
}

func replyEphemeral(
	s *discordgo.Session,
	ic *discordgo.InteractionCreate,
	content string,
) {
	_ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// Scheduler and notifier

func scheduleDaily(hhmm string, loc *time.Location, fn func()) {
	h, m, err := parseHHMM(hhmm)
	if err != nil {
		log.Printf("Invalid RUN_AT %q: %v; using 16:00", hhmm, err)
		h, m = 16, 0
	}
	for {
		now := time.Now().In(loc)
		next := time.Date(
			now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc,
		)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		delay := time.Until(next)
		log.Printf("Next check at %s (%s from now)",
			next.Format(time.RFC1123), delay.Truncate(time.Second))
		timer := time.NewTimer(delay)
		<-timer.C
		fn()
	}
}

func runNotifierOnce(s *discordgo.Session, state *State) {
	// Copy guild IDs
	state.mu.RLock()
	var gids []string
	for gid := range state.Guilds {
		gids = append(gids, gid)
	}
	state.mu.RUnlock()

	for _, gid := range gids {
		notifyGuild(s, state, gid)
	}
}

func notifyGuild(s *discordgo.Session, state *State, guildID string) {
	g := ensureGuild(state, guildID)
	state.mu.RLock()
	channelID := g.ChannelID
	tzName := g.Timezone
	lastPosted := g.LastPosted
	state.mu.RUnlock()

	if channelID == "" {
		return
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil || tzName == "" {
		loc, _ = time.LoadLocation(globalTZ)
	}
	now := time.Now().In(loc)
	todayYYYYMMDD := now.Format("20060102")
	todayKey := now.Format("2006-01-02")

	events, err := fetchUFCEvents(todayYYYYMMDD)
	if err != nil {
		log.Printf("Guild %s: fetch error: %v", guildID, err)
		return
	}
	if len(events) == 0 {
		return
	}

	state.mu.RLock()
	already := lastPosted != nil && lastPosted["ufc"] == todayKey
	state.mu.RUnlock()
	if already {
		return
	}

	msg := buildMessage(events, loc)
	if _, err := s.ChannelMessageSend(channelID, msg); err != nil {
		log.Printf("Guild %s: send message error: %v", guildID, err)
		return
	}

	state.mu.Lock()
	if g.LastPosted == nil {
		g.LastPosted = make(map[string]string)
	}
	g.LastPosted["ufc"] = todayKey
	state.mu.Unlock()
	_ = saveState(statePath, state)
}

func buildMessage(events []ESPNEvent, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("UFC Fight Night Alert:\n")
	for _, e := range events {
		name := e.Name
		if name == "" {
			name = e.ShortName
		}
		tstr := ""
		if t, err := time.Parse(time.RFC3339, e.Date); err == nil {
			tstr = t.In(loc).Format("Mon 3:04 PM")
		}
		if tstr != "" {
			fmt.Fprintf(&b, "• %s — %s\n", name, tstr)
		} else {
			fmt.Fprintf(&b, "• %s\n", name)
		}
	}
	b.WriteString("\nI'll check daily and post here when there's a UFC event.")
	return b.String()
}

// ESPN fetch

func fetchUFCEvents(yyyymmdd string) ([]ESPNEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		"GET",
		fmt.Sprintf(ufcEventsUrl, yyyymmdd),
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ESPN %d: %s", resp.StatusCode, string(body))
	}
	var sb Scoreboard
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, err
	}
	return sb.Events, nil
}

// State helpers

func ensureGuild(state *State, guildID string) *GuildConfig {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Guilds == nil {
		state.Guilds = make(map[string]*GuildConfig)
	}
	g, ok := state.Guilds[guildID]
	if !ok {
		g = &GuildConfig{
			Timezone:   globalTZ,
			LastPosted: make(map[string]string),
		}
		state.Guilds[guildID] = g
	}
	return g
}

func loadState(path string) *State {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return &State{Guilds: make(map[string]*GuildConfig)}
	}
	defer f.Close()
	var st State
	if err := json.NewDecoder(f).Decode(&st); err != nil {
		return &State{Guilds: make(map[string]*GuildConfig)}
	}
	if st.Guilds == nil {
		st.Guilds = make(map[string]*GuildConfig)
	}
	return &st
}

func saveState(path string, st *State) error {
	st.mu.RLock()
	defer st.mu.RUnlock()
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(st); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Utils

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return h, m, nil
}

func getEnv(k, def string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if strings.TrimSpace(v) == "" {
		log.Fatalf("Missing env var: %s", k)
	}
	return v
}
