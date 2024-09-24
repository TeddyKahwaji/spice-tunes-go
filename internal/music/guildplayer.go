package music

import (
	"errors"
	"fmt"
	"time"

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

// TODO: This will handle updating view
// type musicPlayerView struct {
// }

type guildPlayer struct {
	// musicPlayerView *musicPlayerView
	guildID     string
	voiceClient *discordgo.VoiceConnection
	queue       []audiotype.TrackData
	voiceState  VoiceState
	queuePtr    int
	stream      *dca.StreamingSession
}

func (g *guildPlayer) generateMusicPlayerView(session *discordgo.Session, channelID string, timeDuration time.Duration) error {
	if len(g.queue) == 0 {
		return errors.New("cannot generating music player with empty queue")
	}

	currentTrack := g.queue[g.queuePtr]

	musicPlayerEmbed := embeds.MusicPlayerEmbed(currentTrack, timeDuration)

	if g.hasNext() {
		musicPlayerEmbed.Fields = append(musicPlayerEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "`Up Next:`",
			Value:  g.queue[g.queuePtr+1].TrackName,
			Inline: g.queuePtr != 0,
		})
	}

	if g.hasPrevious() {
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
		SkipDisabled: !g.hasNext(),
		BackDisabled: !g.hasPrevious(),
	}

	viewConfig := embeds.ViewConfig{
		Components: &embeds.ComponentHandler{
			MessageComponents: embeds.GetMusicPlayerButtons(buttonsConfig),
		},
		Embeds: []*discordgo.MessageEmbed{musicPlayerEmbed},
	}

	musicPlayerView := embeds.NewView(viewConfig)

	handler := func(messageCustomID string) {
		switch messageCustomID {
		case "BackBtn":
			fmt.Println("implement")
		}
	}

	if err := musicPlayerView.SendView(session, channelID, handler); err != nil {
		return fmt.Errorf("sending music player view: %w", err)
	}

	return nil
}

func (g *guildPlayer) getCurrentSong() string {
	return g.queue[g.queuePtr].Query
}

func (g *guildPlayer) resetQueue() {
	g.queue = []audiotype.TrackData{}
	g.queuePtr = 0
}

func (g *guildPlayer) hasNext() bool {
	return g.queuePtr+1 < len(g.queue)
}

func (g *guildPlayer) hasPrevious() bool {
	return g.queuePtr-1 >= 0
}

func (g *guildPlayer) skip() {
	g.queuePtr += 1
}
