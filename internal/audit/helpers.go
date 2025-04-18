package audit

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/commands"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

// this function is called when instantiating the music cog
func (a *AuditCog) RegisterCommands(session *discordgo.Session) {
	commandMapping := slices.Collect(maps.Values(a.getApplicationCommands()))
	commandsToRegister := funcs.Map(commandMapping, func(ac *commands.ApplicationCommand) *discordgo.ApplicationCommand {
		return ac.CommandConfiguration
	})

	// Fetch existing commands
	existingCommands, err := session.ApplicationCommands(session.State.Application.ID, "")
	if err != nil {
		panic(fmt.Errorf("failed to fetch existing commands: %w", err))
	}

	existingCommandNames := make(map[string]struct{})
	for _, cmd := range existingCommands {
		existingCommandNames[cmd.Name] = struct{}{}
	}

	for _, command := range commandsToRegister {
		if _, exists := existingCommandNames[command.Name]; exists {
			a.logger.Info("Skipping registering command, since it already exists", zap.String("command_name", command.Name))

			continue
		}

		if _, err := session.ApplicationCommandCreate(session.State.Application.ID, "", command); err != nil {
			panic(fmt.Errorf("creating command %s: %w", command.Name, err))
		}
	}

	// This handler will delegate all commands to their respective handler.
	session.AddHandler(a.commandHandler)
	session.AddHandler(a.guildDeleteEvent)
	session.AddHandler(a.guildJoinedEvent)
}

func (a *AuditCog) commandHandler(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	commandMapping := a.getApplicationCommands()
	commandName := interaction.ApplicationCommandData().Name

	command, ok := commandMapping[commandName]
	if !ok {
		return
	}

	if err := command.Handler(session, interaction); err != nil {
		a.logger.Error("an error occurred during when executing command", zap.Error(err), zap.String("command", commandName))
		message, err := session.ChannelMessageSendEmbed(interaction.ChannelID, embeds.UnexpectedErrorEmbed())
		if err != nil {
			a.logger.Warn("failed to send unexpected error message", zap.Error(err))
		}

		_ = util.DeleteMessageAfterTime(session, interaction.ChannelID, message.ID, 30*time.Second)
	}
}

func (a *AuditCog) getApplicationCommands() map[string]*commands.ApplicationCommand {
	return map[string]*commands.ApplicationCommand{}
}
