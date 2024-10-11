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

type Config struct {
	Components *ComponentHandler
	Embeds     []*discordgo.MessageEmbed
	Content    string
	customConfigOptions
}

type customConfigOptions struct {
	logger          *zap.Logger
	deletionEnabled bool
	deletionTimer   time.Duration
}

type ConfigOpts func(*Config)

func WithLogger(logger *zap.Logger) ConfigOpts {
	return func(v *Config) {
		v.logger = logger
	}
}

func WithDeletion(deletionTimer *time.Duration) ConfigOpts {
	return func(v *Config) {
		v.deletionEnabled = true
		v.deletionTimer = *deletionTimer
	}
}

type View struct {
	Config    Config
	message   *discordgo.Message
	MessageID string
	ChannelID string
}

type Handler func(*discordgo.Interaction) error

func NewView(Config Config, opts ...ConfigOpts) *View {
	for _, opt := range opts {
		opt(&Config)
	}

	return &View{
		Config: Config,
	}
}

func (v *View) EditView(viewConfig Config, session *discordgo.Session) error {
	if _, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         v.MessageID,
		Channel:    v.ChannelID,
		Components: &viewConfig.Components.MessageComponents,
		Embeds:     &viewConfig.Embeds,
	}); err != nil {
		return fmt.Errorf("editing complex message: %w", err)
	}

	return nil
}

func (v *View) DeleteView(session *discordgo.Session) error {
	if err := session.ChannelMessageDelete(v.ChannelID, v.MessageID); err != nil {
		return fmt.Errorf("deleting channel message: %w", err)
	}

	return nil
}

func (v *View) SendView(interaction *discordgo.Interaction, session *discordgo.Session, handler Handler) error {
	config := v.Config
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

	if v.Config.deletionEnabled {
		if err := util.DeleteMessageAfterTime(session, channelID, message.ID, v.Config.deletionTimer); err != nil {
			return fmt.Errorf("deleting message after time threshold: %w", err)
		}
	}

	componentHandler := func(_ *discordgo.Session, passedInteraction *discordgo.InteractionCreate) {
		if passedInteraction.Type != discordgo.InteractionMessageComponent {
			return
		}

		if passedInteraction.Message.ID == message.ID {
			if err := handler(passedInteraction.Interaction); err != nil {
				if v.Config.logger != nil {
					v.Config.logger.Error("message component handler failed",
						zap.Error(err), zap.String("messageID", passedInteraction.Message.ID),
						zap.String("customMessageID", passedInteraction.MessageComponentData().CustomID))
				}
			}
		}
	}

	session.AddHandler(componentHandler)

	v.MessageID = message.ID
	v.ChannelID = message.ChannelID
	v.message = message

	return nil
}
