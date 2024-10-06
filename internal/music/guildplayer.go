package music

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"tunes/internal/embeds"
	"tunes/pkg/audiotype"
	"tunes/pkg/views"

	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
)

type voiceState string

const (
	playing    voiceState = "PLAYING"
	paused     voiceState = "PAUSED"
	notPlaying voiceState = "NOT_PLAYING"
)

var (
	errStreamNonExistent = errors.New("no stream exists")
	errNoMusicPlayerView = errors.New("guild player does not have a music player view")
)

type guildPlayer struct {
	guildID     string
	channelID   string
	mu          sync.Mutex
	voiceClient *discordgo.VoiceConnection
	queue       []audiotype.TrackData
	voiceState  voiceState
	queuePtr    atomic.Int32
	stream      *dca.StreamingSession
	doneChannel chan error
	stopChannel chan bool
	view        *views.View
}

func newGuildPlayer(vc *discordgo.VoiceConnection, guildID string, channelID string) *guildPlayer {
	return &guildPlayer{
		voiceClient: vc,
		guildID:     guildID,
		channelID:   channelID,
		queue:       []audiotype.TrackData{},
		voiceState:  notPlaying,
		stopChannel: make(chan bool),
	}
}

func (g *guildPlayer) hasView() bool {
	return g.view != nil
}

func (g *guildPlayer) getMusicPlayerViewConfig() views.ViewConfig {
	currentTrack := g.queue[g.queuePtr.Load()]

	musicPlayerEmbed := embeds.MusicPlayerEmbed(currentTrack)

	if g.hasNext() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Up Next:`",
			Value:  g.queue[g.queuePtr.Load()+1].TrackName,
			Inline: g.queuePtr.Load() != 0,
		})
	}

	if g.hasPrevious() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Previous Song:`",
			Value:  g.queue[g.queuePtr.Load()-1].TrackName,
			Inline: int(g.queuePtr.Load()) != len(g.queue)-1,
		})
	}

	musicPlayerEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{
		URL: currentTrack.TrackImageURL,
	}

	buttonsConfig := embeds.MusicPlayButtonsConfig{
		SkipDisabled:  !g.hasNext(),
		BackDisabled:  !g.hasPrevious(),
		ClearDisabled: !g.hasNext(),
		Resume:        g.isPaused(),
	}

	musicPlayerButtons := embeds.GetMusicPlayerButtons(buttonsConfig)

	return views.ViewConfig{
		Components: &views.ComponentHandler{
			MessageComponents: musicPlayerButtons,
		},
		Embeds: []*discordgo.MessageEmbed{musicPlayerEmbed},
	}
}

func (g *guildPlayer) generateMusicPlayerView(interaction *discordgo.Interaction, session *discordgo.Session) error {
	if len(g.queue) == 0 {
		return errors.New("cannot generate music player with empty queue")
	}

	viewConfig := g.getMusicPlayerViewConfig()
	musicPlayerView := views.NewView(viewConfig)

	handler := func(passedInteraction *discordgo.Interaction) error {
		messageID := passedInteraction.Message.ID
		messageCustomID := passedInteraction.MessageComponentData().CustomID

		switch messageCustomID {
		case "SkipBtn":
			g.skip()
			g.sendStopSignal()
		case "PauseResumeBtn":
			if !g.isPaused() {
				if err := g.pause(); err != nil {
					return fmt.Errorf("pausing: %w", err)
				}
			} else {
				if err := g.resume(); err != nil {
					return fmt.Errorf("resuming: %w", err)
				}
			}

		case "BackBtn":
			g.rewind()
			g.sendStopSignal()
		case "ClearBtn":
			g.resetQueue()
		}

		viewConfig := g.getMusicPlayerViewConfig()

		_, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			ID:         messageID,
			Channel:    passedInteraction.ChannelID,
			Components: &viewConfig.Components.MessageComponents,
			Embeds:     &viewConfig.Embeds,
		})
		if err != nil {
			return fmt.Errorf("editing complex message: %w", err)
		}

		if err := session.InteractionRespond(passedInteraction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
		}); err != nil {
			return fmt.Errorf("sending update message: %w", err)
		}

		return nil
	}

	if err := musicPlayerView.SendView(interaction, session, handler); err != nil {
		return fmt.Errorf("sending music player view: %w", err)
	}

	g.view = musicPlayerView

	return nil
}

func (g *guildPlayer) refreshState(session *discordgo.Session) error {
	if g.view == nil {
		return errNoMusicPlayerView
	}

	viewConfig := g.getMusicPlayerViewConfig()

	_, err := session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         g.view.MessageID,
		Channel:    g.view.ChannelID,
		Components: &viewConfig.Components.MessageComponents,
		Embeds:     &viewConfig.Embeds,
	})
	if err != nil {
		return fmt.Errorf("editing complex message: %w", err)
	}

	return nil
}

func (g *guildPlayer) destroyView(session *discordgo.Session) error {
	if g.view == nil {
		return nil
	}

	defer func() {
		g.view = nil
	}()

	if err := session.ChannelMessageDelete(g.view.ChannelID, g.view.MessageID); err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}

	return nil
}

func (g *guildPlayer) getCurrentSong() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.queue[g.queuePtr.Load()].Query
}

func (g *guildPlayer) getCurrentPointer() int {
	return int(g.queuePtr.Load())
}

func (g *guildPlayer) resetQueue() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = g.queue[:0]
	g.queuePtr.Store(0)
}

func (g *guildPlayer) setVoiceState(voiceState voiceState) {
	g.voiceState = voiceState
}

func (g *guildPlayer) isPlaying() bool {
	return g.voiceState == playing
}

func (g *guildPlayer) isPaused() bool {
	return g.voiceState == paused
}

func (g *guildPlayer) isNotActive() bool {
	return g.voiceState == notPlaying
}

func (g *guildPlayer) addTracks(data ...audiotype.TrackData) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = append(g.queue, data...)
}

func (g *guildPlayer) hasNext() bool {
	return int(g.queuePtr.Load())+1 < len(g.queue)
}

func (g *guildPlayer) isEmptyQueue() bool {
	return len(g.queue) == 0
}

func (g *guildPlayer) pause() error {
	if g.stream == nil {
		return errStreamNonExistent
	}

	g.stream.SetPaused(true)
	g.voiceState = paused

	return nil
}

func (g *guildPlayer) getQueueLength() int {
	return len(g.queue) - g.getCurrentPointer()
}

func (g *guildPlayer) resume() error {
	if g.stream == nil {
		return errStreamNonExistent
	}

	g.stream.SetPaused(false)
	g.voiceState = playing

	return nil
}

func (g *guildPlayer) hasPrevious() bool {
	return g.queuePtr.Load()-1 >= 0
}

func (g *guildPlayer) skip() {
	_ = g.queuePtr.Add(1)
}

func (g *guildPlayer) sendStopSignal() {
	g.stopChannel <- true
}

func (g *guildPlayer) rewind() {
	_ = g.queuePtr.Add(-1)
}
