package music

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	fs "cloud.google.com/go/firestore"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/commands"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/spotify"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/youtube"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type FireStore interface {
	CreateDocument(ctx context.Context, collection string, document string, data interface{}) error
	DeleteDocument(ctx context.Context, collection string, document string) error
	GetDocumentFromCollection(ctx context.Context, collection string, document string) *fs.DocumentRef
	UpdateDocument(ctx context.Context, collection string, document string, data map[string]interface{}) error
}

type TrackDataRetriever interface {
	GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error)
}

var (
	_ TrackDataRetriever = (*spotify.SpotifyClientWrapper)(nil)
	_ TrackDataRetriever = (*youtube.SearchWrapper)(nil)
)

type PlayerCog struct {
	fireStoreClient       FireStore
	userPlaylistRetriever *userPlaylistRetriever
	session               *discordgo.Session
	httpClient            *http.Client
	logger                *zap.Logger
	songSignal            chan *guildPlayer
	guildVoiceStates      map[string]*guildPlayer
	spotifyClient         *spotify.SpotifyClientWrapper
	ytSearchWrapper       *youtube.SearchWrapper
}

type CogConfig struct {
	FireStoreClient      FireStore
	Session              *discordgo.Session
	Logger               *zap.Logger
	HTTPClient           *http.Client
	SpotifyWrapper       *spotify.SpotifyClientWrapper
	YoutubeSearchWrapper *youtube.SearchWrapper
}

func NewPlayerCog(config *CogConfig) (*PlayerCog, error) {
	if config.Logger == nil ||
		config.HTTPClient == nil ||
		config.SpotifyWrapper == nil ||
		config.YoutubeSearchWrapper == nil ||
		config.Session == nil ||
		config.FireStoreClient == nil {
		return nil, errors.New("config was populated with nil value")
	}

	musicCog := &PlayerCog{
		fireStoreClient:       config.FireStoreClient,
		session:               config.Session,
		userPlaylistRetriever: newUserPlaylistRetriever(config.FireStoreClient),
		httpClient:            config.HTTPClient,
		logger:                config.Logger,
		songSignal:            make(chan *guildPlayer),
		guildVoiceStates:      make(map[string]*guildPlayer),
		spotifyClient:         config.SpotifyWrapper,
		ytSearchWrapper:       config.YoutubeSearchWrapper,
	}

	go musicCog.globalPlay()

	return musicCog, nil
}

func (m *PlayerCog) playLikes(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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
	query := options[0].StringValue()

	audioType, err := audiotype.DetermineAudioType(query)
	if err != nil {
		if errors.Is(err, audiotype.ErrUnsupportedAudioType) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed("The provided track type is not supported. Please enter a YouTube or Spotify link.")
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(10*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("sending follow up message: %w", err)
			}

			return nil
		}

		return fmt.Errorf("determining audio type: %w", err)
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*5)
	defer cancelFunc()
	ctx = context.WithValue(ctx, audiotype.ContextKey("requesterName"), interaction.Member.User.Username)

	trackData, err := m.retrieveTracks(ctx, audioType, query)
	if err != nil {
		return fmt.Errorf("retrieving tracks: %w", err)
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

	if trackData == nil {
		return errors.New("unable to retrieve audio data")
	}

	if err := m.addToQueue(session, interaction, trackData, guildPlayer); err != nil {
		return fmt.Errorf("adding to queue: %w", err)
	}

	return nil
}

func (m *PlayerCog) queue(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) skip(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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
		guildPlayer.destroyAllViews(session)
		guildPlayer.resetQueue()
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

func (m *PlayerCog) pause(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) rewind(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) remove(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.remainingQueueLength() == 0 {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("The queue is empty")
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
	position := int(options[0].IntValue()) + guildPlayer.getCurrentPointer()

	trackAtPosition, err := guildPlayer.removeTrack(position)
	if err != nil {
		if errors.Is(err, errInvalidPosition) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed("The position you entered are incorrect, please check the queue and try again")
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
		return fmt.Errorf("removing track from guild player: %w", err)
	}

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		Embeds: embeds.MusicPlayerActionEmbed(fmt.Sprintf("**%s** has been removed from the queue", trackAtPosition.TrackName), *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *PlayerCog) shuffle(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) clear(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) swap(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) spice(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) resume(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) playerview(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

func (m *PlayerCog) playlistCreate(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	options := interaction.ApplicationCommandData().Options
	playlistName := options[0].StringValue()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	userID := interaction.Member.User.ID

	if err := m.userPlaylistRetriever.saveUserPlaylist(ctx, userID, playlistName); err != nil {
		if errors.Is(err, errPlaylistAlreadyExists) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("Playlist `%s` already exists, please use another name", playlistName))
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

		return fmt.Errorf("saving playlist for '%s': %w", userID, err)
	}

	msgData := util.MessageData{
		Embeds: embeds.CreatedUserPlaylistEmbed(playlistName),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		FlagWrapper: &util.FlagWrapper{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}

	if err := util.SendMessage(session, interaction.Interaction, false, msgData); err != nil {
		return fmt.Errorf("interaction response: %w", err)
	}

	return nil
}

func (m *PlayerCog) playlistAdd(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		return fmt.Errorf("deferring message: %w", err)
	}

	options := interaction.ApplicationCommandData().Options
	playlistName, query := options[0].StringValue(), options[1].StringValue()

	audioType, err := audiotype.DetermineAudioType(query)
	if err != nil {
		if errors.Is(err, audiotype.ErrUnsupportedAudioType) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed("The provided track type is not supported. Please enter a YouTube or Spotify link.")
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(10*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("sending follow up message: %w", err)
			}

			return nil
		}

		return fmt.Errorf("determining audio type: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*7)
	defer cancel()

	userID := interaction.Member.User.ID

	ctx = context.WithValue(ctx, audiotype.ContextKey("requesterName"), interaction.Member.User.Username)

	trackData, err := m.retrieveTracks(ctx, audioType, query)
	if err != nil {
		return fmt.Errorf("retrieving tracks: %w", err)
	}

	updatedPlaylist, err := m.userPlaylistRetriever.updateUserPlaylist(ctx, userID, playlistName, trackData.Tracks)
	if err != nil {
		if errors.Is(err, errPlaylistDoesNotExist) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("Playlist `%s` does not exists, please create before attempting to add tracks", playlistName))
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(20*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("interaction response: %w", err)
			}

			return nil
		}

		return fmt.Errorf("getting user playlist: %w", err)
	}

	msgData := util.MessageData{
		Embeds: embeds.AddedTracksToUserPlaylistEmbed(len(trackData.Tracks), len(updatedPlaylist.Tracks), playlistName, interaction.Member),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
	}

	if err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(10*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending follow up message: %w", err)
	}

	return nil
}

func (m *PlayerCog) playlistDelete(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	options := interaction.ApplicationCommandData().Options
	playlistName := options[0].StringValue()
	user := interaction.Member.User

	if err := m.userPlaylistRetriever.deleteUserPlaylist(ctx, user.ID, playlistName); err != nil {
		if errors.Is(err, errPlaylistDoesNotExist) {
			msgData := util.MessageData{
				Embeds: embeds.ErrorMessageEmbed(fmt.Sprintf("Playlist `%s` does not exists", playlistName)),
				FlagWrapper: &util.FlagWrapper{
					Flags: discordgo.MessageFlagsEphemeral,
				},
				Type: discordgo.InteractionResponseChannelMessageWithSource,
			}

			if err := util.SendMessage(session, interaction.Interaction, false, msgData); err != nil {
				return fmt.Errorf("interaction response: %w", err)
			}

			return nil
		}

		return fmt.Errorf("deleting user (%s) playlist: %w", user.ID, err)
	}

	msgData := util.MessageData{
		Embeds: embeds.DeletedUserPlaylistEmbed(playlistName),
		FlagWrapper: &util.FlagWrapper{
			Flags: discordgo.MessageFlagsEphemeral,
		},
		Type: discordgo.InteractionResponseChannelMessageWithSource,
	}

	if err := util.SendMessage(session, interaction.Interaction, false, msgData); err != nil {
		return fmt.Errorf("interaction response: %w", err)
	}

	return nil
}

func (m *PlayerCog) playlistPlay(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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
	playlistName := options[0].StringValue()
	user := interaction.Member.User

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	playlist, err := m.userPlaylistRetriever.getUserPlaylist(ctx, user.ID, playlistName)
	if err != nil {
		if errors.Is(err, errPlaylistDoesNotExist) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("Playlist `%s` does not exists, please create before attempting to add tracks", playlistName))
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(20*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("interaction response: %w", err)
			}

			return nil
		}

		return fmt.Errorf("getting user playlist for %s: %w", user.ID, err)
	}

	if len(playlist.Tracks) == 0 {
		invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("The playlist `%s` is empty. Please add tracks to the playlist before trying to play it", playlistName))
		msgData := util.MessageData{
			Embeds: invalidUsageEmbed,
			Type:   discordgo.InteractionResponseChannelMessageWithSource,
		}

		err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(20*time.Second, interaction.ChannelID))
		if err != nil {
			return fmt.Errorf("interaction response: %w", err)
		}

		return nil
	}

	for _, track := range playlist.Tracks {
		track.Requester = interaction.Member.User.Username
	}

	audioData := &audiotype.Data{
		Tracks: playlist.Tracks,
		PlaylistData: &audiotype.PlaylistData{
			PlaylistName:     fmt.Sprintf("%s's Playlist: %s", user.Username, playlist.Name),
			PlaylistImageURL: user.AvatarURL(""),
		},
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]

	if err := m.addToQueue(session, interaction, audioData, guildPlayer); err != nil {
		return fmt.Errorf("adding to queue: %w", err)
	}

	return nil
}

func (m *PlayerCog) help(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	commandConfigurationList := slices.Collect(maps.Values(m.getApplicationCommands()))
	commands := funcs.Map(commandConfigurationList, func(command *commands.ApplicationCommand) *discordgo.ApplicationCommand {
		return command.CommandConfiguration
	})

	msgData := util.MessageData{
		Embeds: embeds.HelpMenuEmbed(commands),
		Type:   discordgo.InteractionResponseChannelMessageWithSource,
		FlagWrapper: &util.FlagWrapper{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}

	if err := util.SendMessage(session, interaction.Interaction, false, msgData); err != nil {
		return fmt.Errorf("sending help message: %w", err)
	}

	return nil
}
