package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
	httpClient       *http.Client
	logger           *zap.Logger
	songSignal       chan *guildPlayer
	guildVoiceStates map[string]*guildPlayer
	spotifyClient    *spotify.SpotifyWrapper
	ytSearchWrapper  *youtube.YoutubeSearchWrapper
}

type TrackDataRetriever interface {
	GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error)
}

type CogConfig struct {
	Logger               *zap.Logger
	HttpClient           *http.Client
	SpotifyWrapper       *spotify.SpotifyWrapper
	YoutubeSearchWrapper *youtube.YoutubeSearchWrapper
}

func NewMusicPlayerCog(config *CogConfig) (*musicPlayerCog, error) {
	if config.Logger == nil ||
		config.HttpClient == nil ||
		config.SpotifyWrapper == nil ||
		config.YoutubeSearchWrapper == nil {
		return nil, errors.New("config was populated with nil value")
	}

	musicCog := &musicPlayerCog{
		httpClient:       config.HttpClient,
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
			if err := m.playAudio(gp); err != nil {
				m.logger.Warn("error playing audio", zap.String("guild_id", gp.guildID), zap.Error(err))
			}
		}()
	}
}

func (m *musicPlayerCog) downloadTrack(ctx context.Context, audioTrackName string) (*os.File, error) {
	var (
		options         goutubedl.Options
		downloadOptions goutubedl.DownloadOptions
	)

	if strings.Contains(audioTrackName, "ytsearch") {
		options = goutubedl.Options{
			Type:       goutubedl.TypePlaylist,
			HTTPClient: m.httpClient,
			DebugLog:   zap.NewStdLog(m.logger),
			StderrFn:   func(cmd *exec.Cmd) io.Writer { return os.Stderr },
		}

		downloadOptions = goutubedl.DownloadOptions{
			Filter:            "best",
			DownloadAudioOnly: true,
			PlaylistIndex:     1,
		}

	} else {
		options = goutubedl.Options{
			Type:       goutubedl.TypeSingle,
			HTTPClient: m.httpClient,
			DebugLog:   zap.NewStdLog(m.logger),
			StderrFn:   func(cmd *exec.Cmd) io.Writer { return os.Stderr },
		}

		downloadOptions = goutubedl.DownloadOptions{
			Filter:            "best",
			DownloadAudioOnly: true,
		}
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
	if guildPlayer == nil || guildPlayer.voiceClient == nil || guildPlayer.IsEmptyQueue() {
		return nil
	}

	audioTrackName := guildPlayer.GetCurrentSong()

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
	guildPlayer.voiceState = Playing

	for {
		select {
		case err := <-guildPlayer.doneChannel:
			if err != nil {
				if errors.Is(err, io.EOF) {
					if guildPlayer.HasNext() {
						guildPlayer.Skip()
						m.songSignal <- guildPlayer
					} else {
						guildPlayer.voiceState = NotPlaying
						guildPlayer.ResetQueue()
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
	voiceState, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		if errors.Is(err, discordgo.ErrStateNotFound) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("**%s, you must be in a voice channel.**", interaction.Member.User.Username))
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

		return fmt.Errorf("retrieving voice state: %w", err)
	}

	if _, ok := m.guildVoiceStates[interaction.GuildID]; !ok {
		channelVoiceConnection, err := session.ChannelVoiceJoin(interaction.GuildID, voiceState.ChannelID, false, true)
		if err != nil {
			return fmt.Errorf("error unable to join voice channel: %w", err)
		}

		m.guildVoiceStates[interaction.GuildID] = NewGuildPlayer(channelVoiceConnection, interaction.GuildID, interaction.ChannelID)
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

	guildPlayer.AddTracks(trackData.Tracks...)

	if err := guildPlayer.GenerateMusicPlayerView(interaction.Interaction, session); err != nil {
		return fmt.Errorf("generating music player view: %w", err)
	}

	if guildPlayer.voiceState == NotPlaying {
		m.songSignal <- guildPlayer
	}

	return nil
}

func (m *musicPlayerCog) skip(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	_, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		if errors.Is(err, discordgo.ErrStateNotFound) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("**%s, you must be in a voice channel.**", interaction.Member.User.Username))
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
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]
	guildPlayer.SendStopSignal()

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
	var (
		trackData *audiotype.Data
		err       error
	)

	trackData, err = trackRetriever.GetTracksData(ctx, audioType, query)
	if err != nil {
		return nil, fmt.Errorf("retrieving tracks: %w", err)
	}

	return trackData, nil
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
