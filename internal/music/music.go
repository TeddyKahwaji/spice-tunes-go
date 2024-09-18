package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"tunes/pkg/util"

	"github.com/bwmarrin/discordgo"
	"github.com/jung-m/dca"
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
	mu               sync.RWMutex
	logger           *zap.Logger
	songSignal       chan *guildPlayer
	guildVoiceStates map[string]*guildPlayer
}

func NewMusicPlayerCog(logger *zap.Logger) (*musicPlayerCog, error) {
	songSignals := make(chan *guildPlayer)
	musicCog := &musicPlayerCog{
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
			Description: "play a song",
		},
	}
}

func (m *musicPlayerCog) RegisterCommands(session *discordgo.Session) error {
	if _, err := session.ApplicationCommandBulkOverwrite(session.State.Application.ID, "", m.GetCommands()); err != nil {
		return err
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
	guildPlayer.voiceState = Playing
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

	es, err := dca.EncodeFile(audioPath, opts)
	if err != nil {
		m.logger.Error("error encoding file", zap.Error(err))
		return
	}

	defer es.Cleanup()
	doneChan := make(chan error)
	guildPlayer.stream = dca.NewStream(es, guildPlayer.voiceClient, doneChan)
	guildPlayer.voiceState = Playing

	for err := range doneChan {
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(guildPlayer.queue) > 0 {
					m.songSignal <- guildPlayer
				} else {
					guildPlayer.voiceState = NotPlaying
				}
			} else {
				m.logger.Warn("error during audio stream", zap.Error(err))
			}
		} else {
			m.logger.Warn("something went wrong during stream session", zap.Error(err))
		}
	}
}

func (m *musicPlayerCog) Play(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
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
	guildPlayer := m.guildVoiceStates[interaction.GuildID]

	result, err := goutubedl.New(context.Background(), "https://www.youtube.com/watch?v=xjoBP7SDgaY", goutubedl.Options{})
	if err != nil {
		return fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.Download(context.Background(), "best")
	if err != nil {
		log.Fatal(err)
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
		err = m.Play(session, interaction)
	}

	if err != nil {
		m.logger.Error("An error occurred during when executing command", zap.Error(err), zap.String("command", interaction.ApplicationCommandData().Name))
	}

}
