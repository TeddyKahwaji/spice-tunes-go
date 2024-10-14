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

	for _, command := range commandsToRegister {
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

	if command, ok := commandMapping[commandName]; ok {
		if err := command.Handler(session, interaction); err != nil {
			a.logger.Error("an error occurred during when executing command", zap.Error(err), zap.String("command", commandName))
			message, err := session.ChannelMessageSendEmbed(interaction.ChannelID, embeds.UnexpectedErrorEmbed())
			if err != nil {
				a.logger.Warn("failed to send unexpected error message", zap.Error(err))
			}

			_ = util.DeleteMessageAfterTime(session, interaction.ChannelID, message.ID, 30*time.Second)

		}
	}
}

func (a *AuditCog) getApplicationCommands() map[string]*commands.ApplicationCommand {
	return map[string]*commands.ApplicationCommand{}
}
