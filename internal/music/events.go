package music

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	supportErrorLogChannel string = "1094732412845576266"
)

func (m *PlayerCog) guildDeleteEvent(_ *discordgo.Session, guildDeleteEvent *discordgo.GuildDelete) {
	delete(m.guildVoiceStates, guildDeleteEvent.ID)
	m.logger.Info("bot has been kicked from guild", logger.GuildID(guildDeleteEvent.ID))
}

func (m *PlayerCog) handleAutocomplete(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommandAutocomplete {
		return
	}

	const playlistNameOption = "playlist_name"
	option := interaction.ApplicationCommandData().Options[0]
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	if option.Name == playlistNameOption {
		userID := interaction.Member.User.ID
		playlists, err := m.userPlaylistRetriever.getUserPlaylists(ctx, userID)
		if err != nil && !errors.Is(err, errNoPlaylistsCreated) {
			m.logger.Warn("Could not retrieve users playlist", logger.UserID(userID), zap.Error(err))
			return
		}

		query := option.StringValue()

		var suggestions []*discordgo.ApplicationCommandOptionChoice
		for _, playlist := range playlists.Playlists {
			if strings.Contains(strings.ToLower(playlist.Name), strings.ToLower(query)) {
				suggestions = append(suggestions, &discordgo.ApplicationCommandOptionChoice{
					Name:  playlist.Name, // The playlist name shown to the user
					Value: playlist.Name, // The value sent to the command
				})
			}
		}

		// Respond with the suggestions
		err = session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{
				Choices: suggestions,
			},
		})
		if err != nil {
			m.logger.Warn("Sending auto-complete interaction", zap.Error(err))
			return
		}
	}
}

func (m *PlayerCog) voiceStateUpdateEvent(session *discordgo.Session, vc *discordgo.VoiceStateUpdate) {
	if vc == nil || session == nil {
		return
	}

	hasLeft := vc.BeforeUpdate != nil && !vc.Member.User.Bot && vc.ChannelID == ""
	if hasLeft {
		channelMemberCount, err := util.GetVoiceChannelMemberCount(session, vc.BeforeUpdate.GuildID, vc.BeforeUpdate.ChannelID)
		if err != nil {
			m.logger.Error("error getting channel member count", zap.Error(err), logger.ChannelID(vc.BeforeUpdate.ChannelID))
			return
		}

		if channelMemberCount == 1 {
			if botVoiceConnection, ok := session.VoiceConnections[vc.GuildID]; ok && botVoiceConnection.ChannelID == vc.BeforeUpdate.ChannelID {
				if err := botVoiceConnection.Disconnect(); err != nil {
					m.logger.Error("error disconnecting from channel", zap.Error(err))

					return
				}

				if guildPlayer, ok := m.guildVoiceStates[vc.GuildID]; ok {
					guildPlayer.destroyAllViews(session)
					delete(m.guildVoiceStates, vc.GuildID)
				}
			}
		}
	}
}

func (m *PlayerCog) commandHandler(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	commandMapping := m.getApplicationCommands()
	commandName := interaction.ApplicationCommandData().Name

	if command, ok := commandMapping[commandName]; ok {
		if err := command.Handler(session, interaction); err != nil {
			if err := m.reportErrorToSupportChannel(session, interaction, command.CommandConfiguration, err); err != nil {
				m.logger.Warn("could not report error to support channel", zap.Error(err), logger.GuildID(interaction.GuildID))
			}

			m.logger.Error("an error occurred during when executing command", zap.Error(err), zap.String("command", commandName))
			message, err := session.ChannelMessageSendEmbed(interaction.ChannelID, embeds.UnexpectedErrorEmbed())
			if err != nil {
				m.logger.Warn("failed to send unexpected error message", zap.Error(err))
			}

			_ = util.DeleteMessageAfterTime(session, interaction.ChannelID, message.ID, 30*time.Second)

		}
	}
}
