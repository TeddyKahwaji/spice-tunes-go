package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"tunes/internal/embeds"
	"tunes/pkg/models"
	"tunes/pkg/spotify"
	"tunes/pkg/util"

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
}

func NewMusicPlayerCog(logger *zap.Logger, httpClient *http.Client, spotifyWrapper *spotify.SpotifyWrapper) (*musicPlayerCog, error) {
	songSignals := make(chan *guildPlayer)
	musicCog := &musicPlayerCog{
		httpClient:       httpClient,
		logger:           logger,
		songSignal:       songSignals,
		guildVoiceStates: make(map[string]*guildPlayer),
		spotifyClient:    spotifyWrapper,
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

func (m *musicPlayerCog) downloadTrack(ctx context.Context, audioTrackName string) (*os.File, error) {
	result, err := goutubedl.New(ctx, audioTrackName, goutubedl.Options{
		HTTPClient: m.httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.DownloadWithOptions(ctx, goutubedl.DownloadOptions{
		Filter:            "best",
		DownloadAudioOnly: true,
		PlaylistIndex:     1,
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

	return file, nil
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

	file, err := m.downloadTrack(ctx, audioTrackName)
	if err != nil {
		return fmt.Errorf("downloading track: %w", err)
	}

	audioPath := file.Name()

	defer func() {
		if err := file.Close(); err != nil {
			m.logger.Warn("could not close file", zap.Error(err), zap.String("file_name", file.Name()))
		}
	}()

	defer func() {
		if err := util.DeleteFile(audioPath); err != nil {
			m.logger.Warn("could not delete file", zap.Error(err), zap.String("file_name", audioPath))
		}
	}()

	opts := dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 128

	encodingStream, err := dca.EncodeFile(audioPath, opts)
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

				if err := util.DeleteFile(audioPath); err != nil {
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
	audioType, err := models.DetermineAudioType(query)
	if err != nil {
		return fmt.Errorf("determining audio type: %w", err)
	}

	guildPlayer := m.guildVoiceStates[interaction.GuildID]
	trackData, err := m.retrieveTracks(query, audioType)
	if err != nil {
		return err
	}

	m.enqueueItems(guildPlayer, trackData)

	if guildPlayer.voiceState == NotPlaying {
		m.songSignal <- guildPlayer
	}

	return nil
}

func (m *musicPlayerCog) retrieveTracks(query string, audioType models.SupportedAudioType) (*models.Data, error) {
	var (
		trackData *models.Data
		err       error
	)

	if models.IsSpotify(audioType) {
		trackData, err = m.spotifyClient.GetTracksData(query)
	}

	if err != nil {
		return nil, fmt.Errorf("retrieving tracks: %w", err)
	}

	return trackData, nil
}

func (m *musicPlayerCog) enqueueItems(guildPlayer *guildPlayer, trackData *models.Data) {
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
