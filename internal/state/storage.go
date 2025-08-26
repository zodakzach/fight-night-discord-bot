package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	guilds map[string]*GuildConfig
	mu     sync.RWMutex
}

type GuildConfig struct {
	ChannelID  string            `json:"channel_id"`
	Timezone   string            `json:"timezone"`
	LastPosted map[string]string `json:"last_posted"` // sport -> YYYY-MM-DD
}

func Load(path string) *Store {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return &Store{guilds: make(map[string]*GuildConfig)}
	}
	defer f.Close()
	var tmp struct {
		Guilds map[string]*GuildConfig `json:"guilds"`
	}
	if err := json.NewDecoder(f).Decode(&tmp); err != nil {
		return &Store{guilds: make(map[string]*GuildConfig)}
	}
	if tmp.Guilds == nil {
		tmp.Guilds = make(map[string]*GuildConfig)
	}
	return &Store{guilds: tmp.Guilds}
}

func (s *Store) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	payload := struct {
		Guilds map[string]*GuildConfig `json:"guilds"`
	}{Guilds: s.guilds}
	if err := enc.Encode(payload); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) EnsureGuild(guildID string) *GuildConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.guilds == nil {
		s.guilds = make(map[string]*GuildConfig)
	}
	g, ok := s.guilds[guildID]
	if !ok {
		g = &GuildConfig{LastPosted: make(map[string]string)}
		s.guilds[guildID] = g
	}
	return g
}

func (s *Store) GuildIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) GetGuildSettings(guildID string) (channelID, tz string, lastPosted map[string]string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok || g == nil {
		return "", "", nil
	}
	// Return a shallow copy of the map to avoid external mutation races
	var lp map[string]string
	if g.LastPosted != nil {
		lp = make(map[string]string, len(g.LastPosted))
		for k, v := range g.LastPosted {
			lp[k] = v
		}
	}
	return g.ChannelID, g.Timezone, lp
}

func (s *Store) UpdateGuildChannel(guildID, channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.ensureGuildLocked(guildID)
	g.ChannelID = channelID
}

func (s *Store) UpdateGuildTZ(guildID, tz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.ensureGuildLocked(guildID)
	g.Timezone = tz
}

func (s *Store) MarkPosted(guildID, sport, yyyyMmDd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.ensureGuildLocked(guildID)
	if g.LastPosted == nil {
		g.LastPosted = make(map[string]string)
	}
	g.LastPosted[sport] = yyyyMmDd
}

func (s *Store) ensureGuildLocked(guildID string) *GuildConfig {
	if s.guilds == nil {
		s.guilds = make(map[string]*GuildConfig)
	}
	g, ok := s.guilds[guildID]
	if !ok {
		g = &GuildConfig{LastPosted: make(map[string]string)}
		s.guilds[guildID] = g
	}
	return g
}
