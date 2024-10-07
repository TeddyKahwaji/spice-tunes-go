package views

import (
	"fmt"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/util"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type ComponentHandler struct {
	MessageComponents []discordgo.MessageComponent
}

type ViewConfig struct {
	Components *ComponentHandler
	Embeds     []*discordgo.MessageEmbed
	Content    string
	customViewConfigOptions
}

type customViewConfigOptions struct {
	logger          *zap.Logger
	deletionEnabled bool
	deletionTimer   time.Duration
}

type ViewConfigOpts func(*ViewConfig)

func WithLogger(logger *zap.Logger) ViewConfigOpts {
	return func(v *ViewConfig) {
		v.logger = logger
	}
}

func WithDeletion(deletionTimer *time.Duration) ViewConfigOpts {
	return func(v *ViewConfig) {
		v.deletionEnabled = true
		v.deletionTimer = *deletionTimer
	}
}

type View struct {
	viewConfig ViewConfig
	MessageID  string
	ChannelID  string
}

type Handler func(*discordgo.Interaction) error

func NewView(viewConfig ViewConfig, opts ...ViewConfigOpts) *View {
	for _, opt := range opts {
		opt(&viewConfig)
	}

	return &View{
		viewConfig: viewConfig,
	}
}

func (v *View) SendView(interaction *discordgo.Interaction, session *discordgo.Session, handler Handler) error {
	config := v.viewConfig
	channelID := interaction.ChannelID

	messageSendData := &discordgo.WebhookParams{
		Content: config.Content,
	}

	if config.Embeds != nil {
		messageSendData.Embeds = config.Embeds
	}

	if config.Components != nil {
		messageSendData.Components = config.Components.MessageComponents
	}

	// Views assume interaction was defer
	message, err := session.FollowupMessageCreate(interaction, true, messageSendData)
	if err != nil {
		return fmt.Errorf("follow up message create: %w", err)
	}

	if message == nil {
		return fmt.Errorf("empty message: %w", err)
	}

	if v.viewConfig.deletionEnabled {
		if err := util.DeleteMessageAfterTime(session, channelID, message.ID, v.viewConfig.deletionTimer); err != nil {
			return fmt.Errorf("deleting message after time threshold: %w", err)
		}
	}

	componentHandler := func(_ *discordgo.Session, passedInteraction *discordgo.InteractionCreate) {
		if passedInteraction.Type != discordgo.InteractionMessageComponent {
			return
		}

		if passedInteraction.Message.ID == message.ID {
			if err := handler(passedInteraction.Interaction); err != nil {
				if v.viewConfig.logger != nil {
					v.viewConfig.logger.Error("message component handler failed",
						zap.Error(err), zap.String("messageID", passedInteraction.Message.ID),
						zap.String("customMessageID", passedInteraction.MessageComponentData().CustomID))
				}
			}
		}
	}

	session.AddHandler(componentHandler)

	v.MessageID = message.ID
	v.ChannelID = message.ChannelID

	return nil
}
