package embeds

import (
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type ComponentHandler struct {
	MessageComponents []discordgo.MessageComponent
}

type ViewConfig struct {
	Components *ComponentHandler
	Embeds     []*discordgo.MessageEmbed
	Content    string
}

type view struct {
	viewConfig ViewConfig
	messageID  string
}

type Handler func(msgID string)

func NewView(viewConfig ViewConfig) *view {
	return &view{
		viewConfig: viewConfig,
	}
}

func (v *view) EditView() error {
	if v.messageID == "" {
		return errors.New("no view message has previously been sent")
	}

	return nil
}

func (v *view) SendView(session *discordgo.Session, channelID string, handler Handler) error {
	config := v.viewConfig
	messageSendData := &discordgo.MessageSend{
		Content: config.Content,
	}

	if config.Embeds != nil {
		messageSendData.Embeds = config.Embeds
	}

	if config.Components != nil {
		messageSendData.Components = config.Components.MessageComponents
	}

	message, err := session.ChannelMessageSendComplex(channelID, messageSendData)
	if err != nil {
		return fmt.Errorf("sending complex message: %w", err)
	}

	if message == nil {
		return fmt.Errorf("empty message: %w", err)
	}

	componentHandler := func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
		if interaction.Type != discordgo.InteractionMessageComponent {
			return
		}

		if interaction.Message.ID == message.ID {
			handler(interaction.MessageComponentData().CustomID)
		}
	}

	session.AddHandler(componentHandler)
	v.messageID = message.ID

	return nil
}
