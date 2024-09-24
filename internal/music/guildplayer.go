package music

import (
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
	queue       []string
	voiceState  VoiceState
	queuePtr    int
	stream      *dca.StreamingSession
}

func (g *guildPlayer) getCurrentSong() string {
	return g.queue[g.queuePtr]
}

func (g *guildPlayer) resetQueue() {
	g.queue = []string{}
	g.queuePtr = 0
}

func (g *guildPlayer) hasNext() bool {
	return g.queuePtr+1 < len(g.queue)
}

func (g *guildPlayer) skip() {
	g.queuePtr += 1
}
