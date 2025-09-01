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

// requireManageOrAdmin checks for Manage Channels or Admin on a target channel and
// replies with a suitable message when missing or when permission check fails.
// Returns true when the caller has permission; false otherwise (and the caller
// has already been replied to ephemerally).
func requireManageOrAdmin(s *discordgo.Session, ic *discordgo.InteractionCreate, channelID string, notOKMsg string) bool {
	if ic == nil || ic.Member == nil || ic.Member.User == nil {
		_ = sendInteractionResponse(s, ic, "Could not check permissions.")
		return false
	}
	ok, err := hasManageOrAdmin(s, ic.Member.User.ID, channelID)
	if err != nil {
		_ = sendInteractionResponse(s, ic, "Could not check permissions.")
		return false
	}
	if !ok {
		_ = sendInteractionResponse(s, ic, notOKMsg)
		return false
	}
	return true
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
