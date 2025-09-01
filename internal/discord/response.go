package discord

import (
	"github.com/bwmarrin/discordgo"
)

// replyEphemeral wraps sending an ephemeral response for convenience.
func replyEphemeral(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) {
	_ = sendInteractionResponse(s, ic, content)
}

// sendInteractionResponse is a small indirection to allow tests to capture responses
// without performing real HTTP requests via discordgo. Tests may override this var.
var sendInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) error {
	return s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// editInteractionResponse allows tests to capture the final content when using deferred responses.
var editInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate, content string) error {
	_, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{Content: &content})
	return err
}

// deferInteractionResponse allows tests to avoid making real HTTP requests when acknowledging.
var deferInteractionResponse = func(s *discordgo.Session, ic *discordgo.InteractionCreate) error {
	return s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

// editInteractionEmbeds allows tests to capture embed edits without real HTTP calls.
var editInteractionEmbeds = func(s *discordgo.Session, ic *discordgo.InteractionCreate, embeds []*discordgo.MessageEmbed) error {
	_, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{Embeds: &embeds})
	return err
}

// sendChannelMessageComplex is an indirection to send rich messages with content+embeds.
var sendChannelMessageComplex = func(s *discordgo.Session, channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	return s.ChannelMessageSendComplex(channelID, msg)
}
