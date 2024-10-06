package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"tunes/internal/embeds"
	"tunes/pkg/audiotype"
	"tunes/pkg/spotify"
	"tunes/pkg/util"
	"tunes/pkg/youtube"

	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
	"github.com/wader/goutubedl"
	"go.uber.org/zap"
)

type musicPlayerCog struct {
	session          *discordgo.Session
	httpClient       *http.Client
	logger           *zap.Logger
	songSignal       chan *guildPlayer
	guildVoiceStates map[string]*guildPlayer
	spotifyClient    *spotify.SpotifyClientWrapper
	ytSearchWrapper  *youtube.SearchWrapper
}

type TrackDataRetriever interface {
	GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error)
}

type CogConfig struct {
	Session              *discordgo.Session
	Logger               *zap.Logger
	HTTPClient           *http.Client
	SpotifyWrapper       *spotify.SpotifyClientWrapper
	YoutubeSearchWrapper *youtube.SearchWrapper
}

func NewMusicPlayerCog(config *CogConfig) (*musicPlayerCog, error) {
	if config.Logger == nil ||
		config.HTTPClient == nil ||
		config.SpotifyWrapper == nil ||
		config.YoutubeSearchWrapper == nil ||
		config.Session == nil {
		return nil, errors.New("config was populated with nil value")
	}

	musicCog := &musicPlayerCog{
		session:          config.Session,
		httpClient:       config.HTTPClient,
		logger:           config.Logger,
		songSignal:       make(chan *guildPlayer),
		guildVoiceStates: make(map[string]*guildPlayer),
		spotifyClient:    config.SpotifyWrapper,
		ytSearchWrapper:  config.YoutubeSearchWrapper,
	}

	go musicCog.globalPlay()

	return musicCog, nil
}

func (m *musicPlayerCog) GetCommands() []*discordgo.ApplicationCommand {
	return musicPlayerCommands
}

func (m *musicPlayerCog) RegisterCommands(session *discordgo.Session) error {
	if _, err := session.ApplicationCommandBulkOverwrite(session.State.Application.ID, "", m.GetCommands()); err != nil {
		return fmt.Errorf("bulk overwriting commands: %w", err)
	}

	session.AddHandler(m.musicCogCommandHandler)
	session.AddHandler(m.voiceStateUpdate)

	return nil
}

func (m *musicPlayerCog) globalPlay() {
	for gp := range m.songSignal {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					m.logger.Warn("recovered from panic that occurred on playAudio", zap.String("guild_id", gp.guildID))
				}
			}()

			if err := m.playAudio(gp); err != nil {
				m.logger.Warn("error playing audio", zap.String("guild_id", gp.guildID), zap.Error(err))
			}
		}()
	}
}

func (m *musicPlayerCog) downloadTrack(ctx context.Context, audioTrackName string) (*os.File, error) {
	userName := "oauth"
	password := ""

	options := goutubedl.Options{
		Type:       goutubedl.TypeSingle,
		HTTPClient: m.httpClient,
		DebugLog:   zap.NewStdLog(m.logger),
		Username:   &userName,
		Password:   &password,
	}

	downloadOptions := goutubedl.DownloadOptions{
		DownloadAudioOnly: true,
	}

	if strings.Contains(audioTrackName, "ytsearch") {
		options.Type = goutubedl.TypePlaylist
		downloadOptions.PlaylistIndex = 1
	}

	result, err := goutubedl.New(ctx, audioTrackName, options)
	if err != nil {
		return nil, fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.DownloadWithOptions(ctx, downloadOptions)
	if err != nil {
		return nil, fmt.Errorf("downloading youtube data: %w", err)
	}

	defer func() {
		if err := downloadResult.Close(); err != nil {
			m.logger.Warn("couldn't close downloaded result", zap.Error(err))
		}
	}()

	file, err := util.DownloadFileToTempDirectory(downloadResult)
	if err != nil {
		return nil, fmt.Errorf("downloading youtube content to temporary file: %w", err)
	}

	return file, nil
}

func (m *musicPlayerCog) playAudio(guildPlayer *guildPlayer) error {
	// exit if no voice client or no tracks in the queue
	if guildPlayer == nil || guildPlayer.voiceClient == nil || guildPlayer.isEmptyQueue() {
		return nil
	}

	if guildPlayer.hasView() {
		if err := guildPlayer.refreshState(m.session); err != nil {
			m.logger.Warn("unable to refresh music view state", zap.Error(err), zap.String("guild_id", guildPlayer.guildID))
		}
	}

	audioTrackName := guildPlayer.getCurrentSong()

	ctx := context.Background()

	file, err := m.downloadTrack(ctx, audioTrackName)
	if err != nil {
		return fmt.Errorf("downloading result: %w", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			m.logger.Warn("could not close file", zap.Error(err), zap.String("file_name", file.Name()))
		}

		if err := util.DeleteFile(file.Name()); err != nil {
			m.logger.Warn("could not delete file", zap.Error(err), zap.String("file_name", file.Name()))
		}
	}()

	opts := dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 120

	encodingStream, err := dca.EncodeFile(file.Name(), opts)
	if err != nil {
		return fmt.Errorf("encoding file: %w", err)
	}

	defer encodingStream.Cleanup()

	guildPlayer.doneChannel = make(chan error)
	guildPlayer.stream = dca.NewStream(encodingStream, guildPlayer.voiceClient, guildPlayer.doneChannel)
	guildPlayer.setVoiceState(playing)

	for {
		select {
		case err := <-guildPlayer.doneChannel:
			if err != nil {
				if errors.Is(err, io.EOF) {
					if guildPlayer.hasNext() {
						guildPlayer.skip()
						m.songSignal <- guildPlayer
					} else {
						guildPlayer.setVoiceState(notPlaying)
						guildPlayer.resetQueue()
						guildPlayer.destroyView(m.session)
					}
				} else {
					m.logger.Warn("error during audio stream", zap.Error(err))
				}
			}

			return nil

		// receiving signal from stop channel indicates queue ptr has shifted
		case <-guildPlayer.stopChannel:
			m.songSignal <- guildPlayer

			return nil
		}
	}
}

func (m *musicPlayerCog) play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	voiceState, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		return fmt.Errorf("getting voice state: %w", err)
	}

	if _, ok := m.guildVoiceStates[interaction.GuildID]; !ok {
		channelVoiceConnection, err := session.ChannelVoiceJoin(interaction.GuildID, voiceState.ChannelID, false, true)
		if err != nil {
			return fmt.Errorf("error unable to join voice channel: %w", err)
		}

		m.guildVoiceStates[interaction.GuildID] = newGuildPlayer(channelVoiceConnection, interaction.GuildID, interaction.ChannelID)
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

	ctx := context.WithValue(context.Background(), "requesterName", interaction.Member.User.Username)

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
			}

			err := util.SendMessage(session, interaction.Interaction, true, msgData, util.WithDeletion(10*time.Second, interaction.ChannelID))
			if err != nil {
				return fmt.Errorf("sending follow up message: %w", err)
			}

			return nil
		}

		return err
	}

	startingPtr := guildPlayer.getCurrentPointer()
	guildPlayer.addTracks(trackData.Tracks...)

	if guildPlayer.isNotActive() {
		m.songSignal <- guildPlayer
	}

	if guildPlayer.hasView() {
		if err := guildPlayer.refreshState(session); err != nil {
			m.logger.Warn("unable to refresh view state", zap.Error(err), zap.String("guild_id", interaction.GuildID))
		}

		if err := util.SendMessage(session, interaction.Interaction, true, util.MessageData{
			Embeds: embeds.AddedTracksEmbed(trackData, interaction.Member, startingPtr+1),
		}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
			return fmt.Errorf("sending message: %w", err)
		}
	} else {
		if err := guildPlayer.generateMusicPlayerView(interaction.Interaction, session); err != nil {
			m.logger.Error("unable to generate music player view", zap.Error(err), zap.String("guild_id", interaction.GuildID))
		}
	}

	return nil
}

// Helper function to throw error for commands requiring user to be in voice channel
func (m *musicPlayerCog) verifyInChannelAndSendError(session *discordgo.Session, interaction *discordgo.InteractionCreate) (bool, error) {
	_, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		if errors.Is(err, discordgo.ErrStateNotFound) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("%s, you must be in a voice channel.", interaction.Member.User.Username))
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				FlagWrapper: &util.FlagWrapper{
					Flags: discordgo.MessageFlagsEphemeral,
				},
			}

			err := util.SendMessage(session, interaction.Interaction, false, msgData)
			if err != nil {
				return false, fmt.Errorf("interaction response: %w", err)
			}

			return false, nil
		}

		return false, fmt.Errorf("retrieving voice state: %w", err)
	}

	return true, nil
}

func (m *musicPlayerCog) skip(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isEmptyQueue() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
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

	if guildPlayer.hasNext() {
		guildPlayer.skip()
		if err := guildPlayer.refreshState(session); err != nil {
			m.logger.Warn("unable to refresh view state", zap.Error(err), zap.String("guild_id", interaction.GuildID))
		}
	} else {
		guildPlayer.resetQueue()
		if err := guildPlayer.destroyView(session); err != nil {
			m.logger.Warn("could not destroy guild player view", zap.Error(err), zap.String("guild_id", interaction.GuildID))
		}
	}

	guildPlayer.sendStopSignal()

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("⏩ ***Track skipped*** 👍", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *musicPlayerCog) voiceStateUpdate(session *discordgo.Session, vc *discordgo.VoiceStateUpdate) {
	if vc == nil || session == nil {
		return
	}

	hasLeft := vc.BeforeUpdate != nil && !vc.Member.User.Bot && vc.ChannelID == ""
	if hasLeft {
		channelMemberCount, err := util.GetVoiceChannelMemberCount(session, vc.BeforeUpdate.GuildID, vc.BeforeUpdate.ChannelID)
		if err != nil {
			m.logger.Error("error getting channel member count", zap.Error(err), zap.String("channel_id", vc.BeforeUpdate.ChannelID))

			return
		}

		if channelMemberCount <= 1 {
			if botVoiceConnection, ok := session.VoiceConnections[vc.GuildID]; ok && botVoiceConnection.ChannelID == vc.BeforeUpdate.ChannelID {
				if err := botVoiceConnection.Disconnect(); err != nil {
					m.logger.Error("error disconnecting from channel", zap.Error(err))

					return
				}

				delete(m.guildVoiceStates, vc.GuildID)
			}
		}
	}
}

func (m *musicPlayerCog) retrieveTracks(ctx context.Context, audioType audiotype.SupportedAudioType, query string, trackRetriever TrackDataRetriever) (*audiotype.Data, error) {
	trackData, err := trackRetriever.GetTracksData(ctx, audioType, query)
	if err != nil {
		return nil, fmt.Errorf("retrieving tracks: %w", err)
	}

	return trackData, nil
}

func (m *musicPlayerCog) pause(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]

	if !ok || guildPlayer.isEmptyQueue() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
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

	if guildPlayer.isPaused() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("The music is already paused")
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

	if err := guildPlayer.pause(); err != nil {
		return fmt.Errorf("pausing: %w", err)
	}

	guildPlayer.sendStopSignal()
	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), zap.String("guild_id", interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("**Paused** ⏸️", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *musicPlayerCog) rewind(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying in channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]

	if !ok || guildPlayer.isEmptyQueue() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("Nothing is playing in this server")
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

	if !guildPlayer.hasPrevious() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("There is no previous track to go back to")
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

	guildPlayer.rewind()
	guildPlayer.sendStopSignal()

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), zap.String("guild_id", interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("⏪ ***Rewind*** 👍", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *musicPlayerCog) shuffle(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	isInVoiceChannel, err := m.verifyInChannelAndSendError(session, interaction)
	if err != nil {
		return fmt.Errorf("verifying user is in voice channel: %w", err)
	}

	if !isInVoiceChannel {
		return nil
	}

	guildPlayer, ok := m.guildVoiceStates[interaction.GuildID]
	if !ok || guildPlayer.isEmptyQueue() {
		invalidUsageEmbed := embeds.ErrorMessageEmbed("You can't shuffle an empty queue")
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

	guildPlayer.shuffleQueue()

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh view state", zap.Error(err), zap.String("guild_id", interaction.GuildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("**Shuffled queue** 👌", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *musicPlayerCog) clear(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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

	guildPlayer.queue = guildPlayer.queue[:1]

	if err := guildPlayer.refreshState(session); err != nil {
		m.logger.Warn("unable to refresh music view state", zap.Error(err), zap.String("guild_id", guildPlayer.guildID))
	}

	if err = util.SendMessage(session, interaction.Interaction, false, util.MessageData{
		Embeds: embeds.MusicPlayerActionEmbed("💥 **Cleared...** ⏹", *interaction.Member),
	}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	return nil
}

func (m *musicPlayerCog) musicCogCommandHandler(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	var err error

	switch interaction.ApplicationCommandData().Name {
	case "play":
		err = m.play(session, interaction)
	case "skip":
		err = m.skip(session, interaction)
	case "pause":
		err = m.pause(session, interaction)
	case "rewind":
		err = m.rewind(session, interaction)
	case "clear":
		err = m.clear(session, interaction)
	case "shuffle":
		err = m.shuffle(session, interaction)
	}

	if err != nil {
		m.logger.Error("An error occurred during when executing command", zap.Error(err), zap.String("command", interaction.ApplicationCommandData().Name))

		err := util.SendMessage(session, interaction.Interaction, false, util.MessageData{Embeds: embeds.UnexpectedErrorEmbed()},
			util.WithDeletion(time.Second*30, interaction.ChannelID))
		if err != nil {
			m.logger.Error("Failed to send unexpected error message", zap.Error(err))
		}
	}
}
