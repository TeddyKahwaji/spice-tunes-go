package views

import (
	"fmt"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/util"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

// ComponentHandler struct contains a slice of Discord message components.
// MessageComponents is a list of interactive elements like buttons or dropdowns for the Discord message.
type ComponentHandler struct {
	MessageComponents []discordgo.MessageComponent
}

// Config struct represents the configuration for a view, including components, embeds, content, and custom options.
type Config struct {
	Components          *ComponentHandler         // Handles interactive message components.
	Embeds              []*discordgo.MessageEmbed // List of embeds to send with the message.
	Content             string                    // The message content.
	customConfigOptions                           // Struct embedding for additional options.
}

// customConfigOptions struct includes custom configurations like logger, deletion options, and a deletion timer.
type customConfigOptions struct {
	logger          *zap.Logger   // Logger for handling errors and other logs.
	deletionEnabled bool          // Whether the message should be deleted after a certain time.
	deletionTimer   time.Duration // Duration before the message is deleted.
}

// ConfigOpts defines a type for functional options to modify the Config.
type ConfigOpts func(*Config)

// WithLogger is a functional option to set a custom logger in the Config.
func WithLogger(logger *zap.Logger) ConfigOpts {
	return func(v *Config) {
		v.logger = logger
	}
}

// WithDeletion is a functional option to enable deletion and set the deletion timer.
func WithDeletion(deletionTimer time.Duration) ConfigOpts {
	return func(v *Config) {
		v.deletionEnabled = true
		v.deletionTimer = deletionTimer
	}
}

// View struct represents a view that can send, edit, or delete messages with components and embeds.
type View struct {
	Config    Config             // Holds the configuration for the view.
	message   *discordgo.Message // The message object returned from Discord.
	MessageID string             // The ID of the sent message.
	ChannelID string             // The ID of the channel where the message was sent.
}

// Handler is a type alias for a function that handles Discord interactions.
type Handler func(*discordgo.Interaction) error

// NewView creates and returns a new View with the given configuration and optional configuration options.
func NewView(config Config, opts ...ConfigOpts) *View {
	for _, opt := range opts {
		opt(&config) // Apply the functional options to modify the Config.
	}

	return &View{
		Config: config,
	}
}

// EditView updates the message components and embeds of an existing message.
// It uses ChannelMessageEditComplex to edit the message in the channel.
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

// DeleteView deletes the message from the channel using ChannelMessageDelete.
func (v *View) DeleteView(session *discordgo.Session) error {
	if err := session.ChannelMessageDelete(v.ChannelID, v.MessageID); err != nil {
		return fmt.Errorf("deleting channel message: %w", err)
	}

	return nil
}

// SendView sends the view as a follow-up message in response to a Discord interaction.
// It can handle embeds, components, and message deletion after a specified time.
func (v *View) SendView(interaction *discordgo.Interaction, session *discordgo.Session, handler Handler) error {
	config := v.Config
	channelID := interaction.ChannelID

	// Prepare the data for sending the message, including content, embeds, and components.
	messageSendData := &discordgo.WebhookParams{
		Content: config.Content,
	}

	if config.Embeds != nil {
		messageSendData.Embeds = config.Embeds
	}

	if config.Components != nil {
		messageSendData.Components = config.Components.MessageComponents
	}

	// Assumes the interaction was deferred and sends a follow-up message.
	message, err := session.FollowupMessageCreate(interaction, true, messageSendData)
	if err != nil {
		return fmt.Errorf("follow-up message create: %w", err)
	}

	if message == nil {
		return fmt.Errorf("empty message: %w", err)
	}

	// If deletion is enabled, schedule the message for deletion after the specified timer.
	if v.Config.deletionEnabled {
		if err := util.DeleteMessageAfterTime(session, channelID, message.ID, v.Config.deletionTimer); err != nil {
			return fmt.Errorf("deleting message after time threshold: %w", err)
		}
	}

	// Component handler function to handle interactions with the message's components (e.g., buttons).
	componentHandler := func(_ *discordgo.Session, passedInteraction *discordgo.InteractionCreate) {
		if passedInteraction.Type != discordgo.InteractionMessageComponent {
			return
		}

		// Only process interactions for the correct message.
		if passedInteraction.Message.ID == message.ID {
			if err := handler(passedInteraction.Interaction); err != nil {
				// Log an error if the handler fails.
				if v.Config.logger != nil {
					v.Config.logger.Error("message component handler failed",
						zap.Error(err), zap.String("messageID", passedInteraction.Message.ID),
						zap.String("customMessageID", passedInteraction.MessageComponentData().CustomID))
				}
			}
		}
	}

	// Add the component handler to the session.
	session.AddHandler(componentHandler)

	// Store the message and channel IDs for future reference.
	v.MessageID = message.ID
	v.ChannelID = message.ChannelID
	v.message = message

	return nil
}
