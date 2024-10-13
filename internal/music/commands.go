package music

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/util"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type applicationCommandHandler func(*discordgo.Session, *discordgo.InteractionCreate) error

type applicationCommand struct {
	commandConfiguration *discordgo.ApplicationCommand
	handler              applicationCommandHandler
}

func (m *playerCog) playLikes(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	if err := m.joinAndCreateGuildPlayer(session, interaction); err != nil {
		return fmt.Errorf("joining and creating guild player: %w", err)
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	options := interaction.ApplicationCommandData().Options
	selectedUser := options[0].UserValue(session)
	if selectedUser == nil {
		return errors.New("could not retrieve user value from interaction option")
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*5)
	defer cancelFunc()

	tracks, err := guildPlayer.getLikes(ctx, selectedUser.ID)
	if err != nil {
		if errors.Is(err, errUserHasNoLikes) {
			if err := util.SendMessage(session, interaction.Interaction, true, util.MessageData{
				Embeds: embeds.NoLikedTracksFound(selectedUser),
			}, util.WithDeletion(20*time.Second, interaction.ChannelID)); err != nil {
				return fmt.Errorf("sending message: %w", err)
			}
			return nil
		}

		return fmt.Errorf("getting liked tracks: %w", err)
	}

	for _, track := range tracks {
		track.Requester = interaction.Member.User.Username
	}

	audioData := audiotype.Data{
		Tracks: tracks,
		PlaylistData: &audiotype.PlaylistData{
			PlaylistName:     selectedUser.Username + " Liked Tracks",
			PlaylistImageURL: selectedUser.AvatarURL(""),
		},
	}

	if err := m.addToQueue(session, interaction, &audioData, guildPlayer); err != nil {
		return fmt.Errorf("adding to queue: %w", err)
	}

	return nil
}

func (m *playerCog) play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	if err := m.joinAndCreateGuildPlayer(session, interaction); err != nil {
		return fmt.Errorf("joining and creating guild player: %w", err)
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	options := interaction.ApplicationCommandData().Options
	query := options[0].Value.(string)

	audioType, err := audiotype.DetermineAudioType(query)
	if err != nil {
		return fmt.Errorf("determining audio type: %w", err)
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*5)
	defer cancelFunc()
	ctx = context.WithValue(ctx, audiotype.ContextKey("requesterName"), interaction.Member.User.Username)

	var trackData *audiotype.Data
	if audiotype.IsSpotify(audioType) {
		trackData, err = m.retrieveTracks(ctx, audioType, query, m.spotifyClient)
	} else if audiotype.IsYoutube(audioType) || audioType == audiotype.GenericSearch {
		trackData, err = m.retrieveTracks(ctx, audioType, query, m.ytSearchWrapper)
	}

	if err != nil {
		if errors.Is(err, audiotype.ErrSearchQueryNotFound) {
			msgData := util.MessageData{
				Embeds: embeds.NotFoundEmbed(),
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(10*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("sending follow up message: %w", err)
			}

			return nil
		}

		return err
	}

	if err := m.addToQueue(session, interaction, trackData, guildPlayer); err != nil {
		return fmt.Errorf("adding to queue: %w", err)
	}

	return nil
}

func (m *playerCog) queue(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.remainingQueueLength() == 0 {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("The queue is empty")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	if err := guildPlayer.generateMusicQueueView(interaction.Interaction, session); err != nil {
		return fmt.Errorf("generating music queue view: %w", err)
	}

	return nil
}

func (m *playerCog) skip(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isQueueDepleted() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if guildPlayer.hasNext() {
		guildPlayer.skip()
		if err := guildPlayer.refreshState(session); err != nil {
			m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
		}
	} else {
		guildPlayer.resetQueue()
		guildPlayer.destroyAllViews(session)
	}

	guildPlayer.sendStopSignal()

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("⏩ ***Track skipped*** 👍", *interaction.Member),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) pause(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]

	if !ok || guildPlayer.isQueueDepleted() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if guildPlayer.isPaused() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("The music is already paused")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if err := guildPlayer.pause(); err != nil {
		return fmt.Errorf("pausing: %w", err)
	}

	guildPlayer.sendStopSignal()
	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("**Paused** ⏸️", *interaction.Member),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) rewind(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]

	if !ok || guildPlayer.isQueueDepleted() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if !guildPlayer.hasPrevious() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("There is no previous track to go back to")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	guildPlayer.rewind()
	guildPlayer.sendStopSignal()

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		Embeds: embeds.MusicPlayerActionEmbed("⏪ ***Rewind*** 👍", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) shuffle(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isEmptyQueue() || guildPlayer.remainingQueueLength() == 0 {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("You can't shuffle an empty queue")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	guildPlayer.shuffleQueue()

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		Embeds: embeds.MusicPlayerActionEmbed("**Shuffled queue** 👌", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) clear(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || !guildPlayer.hasNext() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("There is no queue to clear")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	guildPlayer.clearUpcomingTracks()

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh views", zap.Error(err), logger.GuildID(guildPlayer.guildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		Embeds: embeds.MusicPlayerActionEmbed("💥 **Cleared...** ⏹", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) swap(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isEmptyQueue() || guildPlayer.remainingQueueLength() == 0 {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("You can't swap from an empty queue")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	options := interaction.ApplicationCommandData().Options

	firstPosition, secondPosition := int(options[0].IntValue()), int(options[1].IntValue())

	if err := guildPlayer.swap(guildPlayer.getCurrentPointer()+firstPosition, guildPlayer.getCurrentPointer()+secondPosition); err != nil {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("The positions you entered are incorrect, please check the queue and try again")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	newFirstTrack := guildPlayer.getTrackAtPosition(guildPlayer.getCurrentPointer() + firstPosition)
	newSecondTrack := guildPlayer.getTrackAtPosition(guildPlayer.getCurrentPointer() + secondPosition)

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh views", zap.Error(err), logger.GuildID(guildPlayer.guildID))
	}

	if err := util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		Embeds: embeds.TracksSwappedEmbed(interaction.Member, newSecondTrack, firstPosition, newFirstTrack, secondPosition),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) spice(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isQueueDepleted() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
		msgData := util.MessageData{
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	ctx := context.WithValue(context.Background(), audiotype.ContextKey("requesterName"), interaction.Member.User.Username)

	ctx, cancelFunc := context.WithTimeout(ctx, time.Second*5)
	defer cancelFunc()

	currentSongPlaying := guildPlayer.getCurrentSong().TrackName
	recommendationLimit := 20

	recommendations, err := m.spotifyClient.GetRecommendation(ctx, currentSongPlaying, recommendationLimit)
	if err != nil {
		return fmt.Errorf("getting recommendations: %w", err)
	}
	addedPosition := guildPlayer.remainingQueueLength() + 1
	guildPlayer.addTracks(recommendations...)

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh views", zap.Error(err), logger.GuildID(guildPlayer.guildID))
	}

	spiceEmbed := embeds.SpiceEmbed(len(recommendations), addedPosition, interaction.Member)

	if err := util.SendMessage(session, interaction.Interaction, true, util.MessageData{
		Embeds: spiceEmbed,
	}, util.WithDeletion(20*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending spice embed message: %w", err)
	}

	return nil
}

func (m *playerCog) resume(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || !guildPlayer.isPaused() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("There is no track currently paused to resume.")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if err := guildPlayer.resume(); err != nil {
		return fmt.Errorf("resuming guild player: %w", err)
	}

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh views", zap.Error(err), logger.GuildID(guildPlayer.guildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("⏯️ **Resuming** 👍", *interaction.Member),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *playerCog) playerview(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isQueueDepleted() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
			FlagWrapper: &util.FlagWrapper{
				Flags: discordgo.MessageFlagsEphemeral,
			},
		}

		err := util.SendMessage(session, interaction.Interaction, false, msgData)
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	if err := guildPlayer.generateMusicPlayerView(interaction.Interaction, session); err != nil {
		return fmt.Errorf("generating music player view: %w", err)
	}

	return nil
}
