package embeds

import (
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
}

type Handler func(msgID string)

type messageCreationHandler func(session *discordgo.Session, interaction *discordgo.InteractionCreate)

func NewView(viewConfig ViewConfig) *view {
	return &view{
		viewConfig: viewConfig,
	}
}

func (v *view) SendView(session *discordgo.Session, channelID string, handler Handler) error {
	config := v.viewConfig
	messageSendData := &discordgo.MessageSend{
		Content: config.Content,
	}

	if config.Embeds != nil {
		if len(config.Embeds) > 1 {
			messageSendData.Embeds = config.Embeds
		} else {
			messageSendData.Embed = config.Embeds[0]
		}
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
			handler(message.ID)
		}
	}

	session.AddHandler(componentHandler)

	return nil
}
