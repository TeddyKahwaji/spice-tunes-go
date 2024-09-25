package music

import (
	"errors"
	"fmt"

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

type guildPlayer struct {
	guildID     string
	voiceClient *discordgo.VoiceConnection
	queue       []audiotype.TrackData
	voiceState  VoiceState
	queuePtr    int
	stream      *dca.StreamingSession
}

func NewGuildPlayer(vc *discordgo.VoiceConnection, guildID string) *guildPlayer {
	return &guildPlayer{
		voiceClient: vc,
		guildID:     guildID,
		queue:       []audiotype.TrackData{},
		voiceState:  NotPlaying,
		queuePtr:    0,
	}
}

func (g *guildPlayer) getMusicPlayerViewConfig() views.ViewConfig {
	currentTrack := g.queue[g.queuePtr]

	musicPlayerEmbed := embeds.MusicPlayerEmbed(currentTrack)

	if g.HasNext() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Up Next:`",
			Value:  g.queue[g.queuePtr+1].TrackName,
			Inline: g.queuePtr != 0,
		})
	}

	if g.HasPrevious() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Previous Song:`",
			Value:  g.queue[g.queuePtr-1].TrackName,
			Inline: g.queuePtr != len(g.queue)-1,
		})
	}

	musicPlayerEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{
		URL: currentTrack.TrackImageURL,
	}

	buttonsConfig := embeds.MusicPlayButtonsConfig{
		SkipDisabled: !g.HasNext(),
		BackDisabled: !g.HasPrevious(),
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
		case "BackBtn":
			g.Rewind()
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
	return g.queue[g.queuePtr].Query
}

func (g *guildPlayer) ResetQueue() {
	g.queue = []audiotype.TrackData{}
	g.queuePtr = 0
}

func (g *guildPlayer) HasNext() bool {
	return g.queuePtr+1 < len(g.queue)
}

func (g *guildPlayer) HasPrevious() bool {
	return g.queuePtr-1 >= 0
}

func (g *guildPlayer) Skip() {
	g.queuePtr += 1
}

func (g *guildPlayer) Rewind() {
	g.queuePtr -= 1
}
