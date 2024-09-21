package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
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
}

func NewMusicPlayerCog(logger *zap.Logger, httpClient *http.Client) (*musicPlayerCog, error) {
	songSignals := make(chan *guildPlayer)
	musicCog := &musicPlayerCog{
		httpClient:       httpClient,
		logger:           logger,
		songSignal:       songSignals,
		guildVoiceStates: make(map[string]*guildPlayer),
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
		go m.playAudio(gp)
	}
}

func (m *musicPlayerCog) playAudio(guildPlayer *guildPlayer) {
	if guildPlayer.voiceClient == nil || len(guildPlayer.queue) == 0 {
		return
	}

	m.mu.Lock()
	audioPath := guildPlayer.queue[0]
	guildPlayer.queue = guildPlayer.queue[1:]
	m.mu.Unlock()

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
		m.logger.Error("error encoding file", zap.Error(err))

		return
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
}

func (m *musicPlayerCog) play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	if _, ok := m.guildVoiceStates[interaction.GuildID]; !ok {
		voiceState, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
		if err != nil {
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
	// options := interaction.ApplicationCommandData().Options
	// query := options[0].Value.(string)
	guildPlayer := m.guildVoiceStates[interaction.GuildID]
	ctx := context.Background()

	result, err := goutubedl.New(ctx, "https://www.youtube.com/watch?v=xjoBP7SDgaY", goutubedl.Options{
		Type:       goutubedl.TypeSingle,
		HTTPClient: m.httpClient,
	})
	if err != nil {
		return fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.DownloadWithOptions(ctx, goutubedl.DownloadOptions{
		Filter:            "best",
		DownloadAudioOnly: true,
	})
	if err != nil {
		return fmt.Errorf("downloading youtube data: %w", err)
	}

	defer func() {
		if err := downloadResult.Close(); err != nil {
			m.logger.Warn("couldn't close downloaded result", zap.Error(err))
		}
	}()

	file, err := util.DownloadFileToTempDirectory(downloadResult)
	if err != nil {
		return fmt.Errorf("error downloading youtube content to temporary file: %w", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			m.logger.Warn("could not close file", zap.Error(err), zap.String("file_name", file.Name()))
		}
	}()

	m.mu.Lock()
	guildPlayer.queue = append(guildPlayer.queue, file.Name())
	defer m.mu.Unlock()

	m.songSignal <- guildPlayer

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
	}

	if err != nil {
		m.logger.Error("An error occurred during when executing command", zap.Error(err), zap.String("command", interaction.ApplicationCommandData().Name))
	}

}
