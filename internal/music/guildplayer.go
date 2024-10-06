package music

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	views "tunes/internal"
	"tunes/internal/embeds"
	"tunes/pkg/audiotype"

	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
)

type VoiceState string

const (
	Playing    VoiceState = "PLAYING"
	Paused     VoiceState = "PAUSED"
	NotPlaying VoiceState = "NOT_PLAYING"
)

var ErrStreamNonExistent = errors.New("no stream exists")

type guildPlayer struct {
	guildID     string
	channelID   string
	mu          sync.Mutex
	voiceClient *discordgo.VoiceConnection
	queue       []audiotype.TrackData
	voiceState  VoiceState
	queuePtr    atomic.Int32
	stream      *dca.StreamingSession
	doneChannel chan error
	stopChannel chan bool
}

func NewGuildPlayer(vc *discordgo.VoiceConnection, guildID string, channelID string) *guildPlayer {
	return &guildPlayer{
		voiceClient: vc,
		guildID:     guildID,
		channelID:   channelID,
		queue:       []audiotype.TrackData{},
		voiceState:  NotPlaying,
		stopChannel: make(chan bool),
	}
}

func (g *guildPlayer) getMusicPlayerViewConfig() views.ViewConfig {
	currentTrack := g.queue[g.queuePtr.Load()]

	musicPlayerEmbed := embeds.MusicPlayerEmbed(currentTrack)

	if g.HasNext() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Up Next:`",
			Value:  g.queue[g.queuePtr.Load()+1].TrackName,
			Inline: g.queuePtr.Load() != 0,
		})
	}

	if g.HasPrevious() {
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
		SkipDisabled:  !g.HasNext(),
		BackDisabled:  !g.HasPrevious(),
		ClearDisabled: !g.HasNext(),
		Resume:        g.IsPaused(),
	}

	musicPlayerButtons := embeds.GetMusicPlayerButtons(buttonsConfig)

	return views.ViewConfig{
		Components: &views.ComponentHandler{
			MessageComponents: musicPlayerButtons,
		},
		Embeds: []*discordgo.MessageEmbed{musicPlayerEmbed},
	}
}

func (g *guildPlayer) GenerateMusicPlayerView(interaction *discordgo.Interaction, session *discordgo.Session) error {
	if len(g.queue) == 0 {
		return errors.New("cannot generating music player with empty queue")
	}

	viewConfig := g.getMusicPlayerViewConfig()
	musicPlayerView := views.NewView(viewConfig)

	handler := func(passedInteraction *discordgo.Interaction) error {
		messageID := passedInteraction.Message.ID
		messageCustomID := passedInteraction.MessageComponentData().CustomID

		switch messageCustomID {
		case "SkipBtn":
			g.Skip()
			g.SendStopSignal()
		case "PauseResumeBtn":
			if !g.IsPaused() {
				if err := g.Pause(); err != nil {
					return fmt.Errorf("pausing: %w", err)
				}
			} else {
				if err := g.Resume(); err != nil {
					return fmt.Errorf("resuming: %w", err)
				}
			}

		case "BackBtn":
			g.Rewind()
			g.SendStopSignal()
		case "ClearBtn":
			g.ResetQueue()
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

	return nil
}

func (g *guildPlayer) GetCurrentSong() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.queue[g.queuePtr.Load()].Query
}

func (g *guildPlayer) ResetQueue() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = g.queue[:0]
	g.queuePtr.Store(0)
}

func (g *guildPlayer) SetVoiceState(voiceState VoiceState) {
	g.voiceState = voiceState
}

func (g *guildPlayer) IsPlaying() bool {
	return g.voiceState == Playing
}

func (g *guildPlayer) IsPaused() bool {
	return g.voiceState == Paused
}

func (g *guildPlayer) IsNotActive() bool {
	return g.voiceState == NotPlaying
}

func (g *guildPlayer) AddTracks(data ...audiotype.TrackData) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = append(g.queue, data...)
}

func (g *guildPlayer) HasNext() bool {
	return int(g.queuePtr.Load())+1 < len(g.queue)
}

func (g *guildPlayer) IsEmptyQueue() bool {
	return len(g.queue) == 0
}

func (g *guildPlayer) Pause() error {
	if g.stream == nil {
		return ErrStreamNonExistent
	}

	g.stream.SetPaused(true)
	g.voiceState = Paused

	return nil
}

func (g *guildPlayer) Resume() error {
	if g.stream == nil {
		return ErrStreamNonExistent
	}

	g.stream.SetPaused(false)
	g.voiceState = Playing

	return nil
}

func (g *guildPlayer) HasPrevious() bool {
	return g.queuePtr.Load()-1 >= 0
}

func (g *guildPlayer) Skip() {
	_ = g.queuePtr.Add(1)
}

func (g *guildPlayer) SendStopSignal() {
	g.stopChannel <- true
}

func (g *guildPlayer) Rewind() {
	_ = g.queuePtr.Add(-1)
}
