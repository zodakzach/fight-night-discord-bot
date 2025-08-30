package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zodakzach/fight-night-discord-bot/internal/config"
	"github.com/zodakzach/fight-night-discord-bot/internal/state"
)

// hasManageOrAdmin checks whether the given user has Manage Channels or Admin
// permission in the target channel.
func hasManageOrAdmin(s *discordgo.Session, userID, channelID string) (bool, error) {
	perms, err := s.UserChannelPermissions(userID, channelID)
	if err != nil {
		return false, err
	}
	if perms&discordgo.PermissionManageChannels != 0 || perms&discordgo.PermissionAdministrator != 0 {
		return true, nil
	}
	return false, nil
}

// guildLocation resolves the guild's configured timezone (falling back to
// global config when unset/invalid) and returns the location and tz name.
func guildLocation(st *state.Store, cfg config.Config, guildID string) (*time.Location, string) {
	_, tzName, _ := st.GetGuildSettings(guildID)
	if tzName == "" {
		tzName = cfg.TZ
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.Local
	}
	return loc, tzName
}
