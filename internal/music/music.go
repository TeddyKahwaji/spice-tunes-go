package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
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

type VoiceState string

const (
	Playing    VoiceState = "PLAYING"
	NotPlaying VoiceState = "NOT_PLAYING"
)

type guildPlayer struct {
	guildID     string
	voiceClient *discordgo.VoiceConnection
	queue       []string
	voiceState  VoiceState
	stream      *dca.StreamingSession
}

type musicPlayerCog struct {
	httpClient       *http.Client
	mu               sync.RWMutex
	logger           *zap.Logger
	songSignal       chan *guildPlayer
	guildVoiceStates map[string]*guildPlayer
	spotifyClient    *spotify.SpotifyWrapper
	ytSearchWrapper  *youtube.YoutubeSearchWrapper
}

type track struct {
	file     *os.File
	duration time.Duration
}

type TrackDataRetriever interface {
	GetTracksData(audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error)
}

type MusicCogConfig struct {
	Logger               *zap.Logger
	HttpClient           *http.Client
	SpotifyWrapper       *spotify.SpotifyWrapper
	YoutubeSearchWrapper *youtube.YoutubeSearchWrapper
}

func NewMusicPlayerCog(config *MusicCogConfig) (*musicPlayerCog, error) {
	if config.Logger == nil ||
		config.HttpClient == nil ||
		config.SpotifyWrapper == nil ||
		config.YoutubeSearchWrapper == nil {
		return nil, errors.New("config was populated with nil value")
	}

	songSignals := make(chan *guildPlayer)
	musicCog := &musicPlayerCog{
		httpClient:       config.HttpClient,
		logger:           config.Logger,
		songSignal:       songSignals,
		guildVoiceStates: make(map[string]*guildPlayer),
		spotifyClient:    config.SpotifyWrapper,
		ytSearchWrapper:  config.YoutubeSearchWrapper,
	}

	go musicCog.globalPlay()

	return musicCog, nil
}

func (m *musicPlayerCog) GetCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "play",
			Description: "Plays desired song/playlist",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "query",
					Description: "Song/playlist search query",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
			},
		},
	}
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
				m.logger.Warn("error playing audio", zap.String("guild_id", gp.guildID))
			}
		}()
	}
}

func (m *musicPlayerCog) downloadTrack(ctx context.Context, audioTrackName string) (*track, error) {
	result, err := goutubedl.New(ctx, audioTrackName, goutubedl.Options{
		Type:       goutubedl.TypeSingle,
		HTTPClient: m.httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.DownloadWithOptions(ctx, goutubedl.DownloadOptions{
		Filter:            "best",
		DownloadAudioOnly: true,
	})
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
		return nil, fmt.Errorf("error downloading youtube content to temporary file: %w", err)
	}

	return &track{
		file:     file,
		duration: time.Duration(result.Info.Duration),
	}, nil
}

func (m *musicPlayerCog) playAudio(guildPlayer *guildPlayer) error {
	// exit if no voice client or no tracks in the queue
	if guildPlayer.voiceClient == nil || len(guildPlayer.queue) == 0 {
		return nil
	}

	m.mu.Lock()
	audioTrackName := guildPlayer.queue[0]
	guildPlayer.queue = guildPlayer.queue[1:]
	m.mu.Unlock()

	ctx := context.Background()

	track, err := m.downloadTrack(ctx, audioTrackName)
	if err != nil {
		return fmt.Errorf("downloading track: %w", err)
	}

	file := track.file
	filePath := track.file.Name()

	defer func() {
		if err := file.Close(); err != nil {
			m.logger.Warn("could not close file", zap.Error(err), zap.String("file_name", file.Name()))
		}

		if err := util.DeleteFile(filePath); err != nil {
			m.logger.Warn("could not delete file", zap.Error(err), zap.String("file_name", filePath))
		}
	}()

	opts := dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 128

	encodingStream, err := dca.EncodeFile(filePath, opts)
	if err != nil {
		return fmt.Errorf("encoding file: %w", err)
	}

	defer encodingStream.Cleanup()

	doneChan := make(chan error)
	guildPlayer.stream = dca.NewStream(encodingStream, guildPlayer.voiceClient, doneChan)
	guildPlayer.voiceState = Playing

	for err := range doneChan {
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(guildPlayer.queue) > 0 {
					m.songSignal <- guildPlayer
				} else {
					guildPlayer.voiceState = NotPlaying
				}

				if err := util.DeleteFile(filePath); err != nil {
					m.logger.Warn("could not delete file", zap.Error(err))
				}
			} else {
				m.logger.Warn("error during audio stream", zap.Error(err))
			}
		} else {
			m.logger.Warn("something went wrong during stream session", zap.Error(err))
		}
	}

	return nil
}

func (m *musicPlayerCog) play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	if _, ok := m.guildVoiceStates[interaction.GuildID]; !ok {
		voiceState, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
		if err != nil {
			if errors.Is(err, discordgo.ErrStateNotFound) {
				invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("**%s, you must be in a voice channel.**", interaction.Member.User.Username))
				err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Embeds: []*discordgo.MessageEmbed{invalidUsageEmbed},
						Flags:  discordgo.MessageFlagsEphemeral,
					},
				})
				if err != nil {
					return fmt.Errorf("interaction response: %w", err)
				}

				return nil
			}

			return fmt.Errorf("retrieving voice state: %w", err)
		}

		channelVoiceConnection, err := session.ChannelVoiceJoin(interaction.GuildID, voiceState.ChannelID, false, true)
		if err != nil {
			return fmt.Errorf("error unable to join voice channel: %w", err)
		}

		m.guildVoiceStates[interaction.GuildID] = &guildPlayer{
			voiceClient: channelVoiceConnection,
			guildID:     interaction.GuildID,
			queue:       []string{},
			voiceState:  NotPlaying,
		}
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

	var trackData *audiotype.Data
	if audiotype.IsSpotify(audioType) {
		trackData, err = m.retrieveTracks(audioType, query, m.spotifyClient)
	} else if audiotype.IsYoutube(audioType) || audioType == audiotype.GenericSearch {
		trackData, err = m.retrieveTracks(audioType, query, m.ytSearchWrapper)
	}

	if err != nil {
		return err
	}

	m.enqueueItems(guildPlayer, trackData)

	if guildPlayer.voiceState == NotPlaying {
		m.songSignal <- guildPlayer
	}

	return nil
}

func (m *musicPlayerCog) voiceStateUpdate(session *discordgo.Session, vc *discordgo.VoiceStateUpdate) {
	hasLeft := vc.BeforeUpdate != nil && !vc.Member.User.Bot && vc.ChannelID == ""
	if hasLeft {
		m.mu.Lock()
		defer m.mu.Unlock()
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

func (m *musicPlayerCog) retrieveTracks(audioType audiotype.SupportedAudioType, query string, trackRetriever TrackDataRetriever) (*audiotype.Data, error) {
	var (
		trackData *audiotype.Data
		err       error
	)

	trackData, err = trackRetriever.GetTracksData(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("retrieving tracks: %w", err)
	}

	return trackData, nil
}

func (m *musicPlayerCog) enqueueItems(guildPlayer *guildPlayer, trackData *audiotype.Data) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, track := range trackData.Tracks {
		guildPlayer.queue = append(guildPlayer.queue, track.Query)
	}
}

func (m *musicPlayerCog) musicCogCommandHandler(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	var err error

	switch interaction.ApplicationCommandData().Name {
	case "play":
		err = m.play(session, interaction)
	}

	if err != nil {
		m.logger.Error("An error occurred during when executing command", zap.Error(err), zap.String("command", interaction.ApplicationCommandData().Name))
	}
}
