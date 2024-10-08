package music

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/util"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/views"

	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
	"go.uber.org/zap"
)

type voiceState string

const (
	playing    voiceState = "PLAYING"
	paused     voiceState = "PAUSED"
	notPlaying voiceState = "NOT_PLAYING"
)

const (
	guildCollection    string = "guilds"
	userDataCollection string = "user-data"
	likedTracksPath    string = "liked_tracks"
)

var (
	errStreamNonExistent = errors.New("no stream exists")
	errNoMusicPlayerView = errors.New("guild player does not have a music player view")
	errEmptyQueue        = errors.New("queue is empty")
	errInvalidPosition   = errors.New("position provided is out of bounds")
)

type likedTrack struct {
	SpotifyTrackID string `firestore:"spotifyTrackId"`
}

type userData struct {
	LikedTracks    []likedTrack          `firestore:"liked_tracks"`
	SavedPlaylists []audiotype.TrackData `firestore:"playlists"`
}

type TrackSearcher interface {
	SearchTrack(trackName string) (string, error)
}

type guildPlayer struct {
	guildID         string
	channelID       string
	mu              sync.Mutex
	voiceClient     *discordgo.VoiceConnection
	queue           []*audiotype.TrackData
	voiceState      voiceState
	queuePtr        atomic.Int32
	stream          *dca.StreamingSession
	doneChannel     chan error
	stopChannel     chan bool
	view            *views.View
	fireStoreClient FireStore
	trackSearcher   TrackSearcher
}

func newGuildPlayer(vc *discordgo.VoiceConnection, guildID string, channelID string, fireStoreClient FireStore, trackSearcher TrackSearcher) *guildPlayer {
	return &guildPlayer{
		voiceClient:     vc,
		guildID:         guildID,
		channelID:       channelID,
		queue:           []*audiotype.TrackData{},
		voiceState:      notPlaying,
		stopChannel:     make(chan bool),
		fireStoreClient: fireStoreClient,
		trackSearcher:   trackSearcher,
	}
}

func (g *guildPlayer) hasView() bool {
	return g.view != nil
}

func (g *guildPlayer) getMusicPlayerViewConfig() views.Config {
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
		SkipDisabled:  !g.hasNext() || g.isPaused(),
		BackDisabled:  !g.hasPrevious() || g.isPaused(),
		ClearDisabled: !g.hasNext(),
		Resume:        g.isPaused(),
	}

	musicPlayerButtons := embeds.GetMusicPlayerButtons(buttonsConfig)

	return views.Config{
		Components: &views.ComponentHandler{
			MessageComponents: musicPlayerButtons,
		},
		Embeds: []*discordgo.MessageEmbed{musicPlayerEmbed},
	}
}

// This is a best case effort, if the song doesn't exist we don't like but don't propagate an error to the user
// this will only return errors in non-404 case.
func (g *guildPlayer) likeCurrentSong(ctx context.Context, userID string) error {
	currentSong := g.getCurrentSong()

	spotifyTrackID, err := g.trackSearcher.SearchTrack(currentSong.TrackName)
	if err != nil {
		return fmt.Errorf("searching for track: %w", err)
	}

	trackToAdd := likedTrack{
		SpotifyTrackID: spotifyTrackID,
	}

	docRef, err := g.fireStoreClient.GetDocumentFromCollection(ctx, guildCollection, g.guildID).
		Collection(userDataCollection).
		Doc(userID).
		Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			if _, err := docRef.Ref.Set(ctx, userData{
				LikedTracks:    []likedTrack{trackToAdd},
				SavedPlaylists: []audiotype.TrackData{},
			}); err != nil {
				return fmt.Errorf("setting user data: %w", err)
			}

			return nil
		}

		return fmt.Errorf("getting document from collection: %w", err)
	}

	if _, err := docRef.Ref.Update(ctx, []firestore.Update{
		{
			Path:  likedTracksPath,
			Value: firestore.ArrayUnion(trackToAdd),
		},
	}); err != nil {
		return fmt.Errorf("updating document: %w", err)
	}

	return nil
}

func (g *guildPlayer) generateMusicQueueView(interaction *discordgo.Interaction, session *discordgo.Session, logger *zap.Logger) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	guild, err := util.GetGuild(session, interaction.GuildID)
	if err != nil {
		return fmt.Errorf("getting guild: %w", err)
	}

	separator := 8
	paginationConfig := views.NewPaginatedConfig(g.queue[g.getCurrentPointer()+1:], separator)

	getQueueEmbed := func(tracks []*audiotype.TrackData, pageNumber int, separator int) *discordgo.MessageEmbed {
		return embeds.QueueEmbed(tracks, pageNumber, separator, guild)
	}

	viewConfig := paginationConfig.GetViewConfig(getQueueEmbed)
	handler := func(passedInteraction *discordgo.Interaction) error {
		messageID := passedInteraction.Message.ID
		viewConfig := paginationConfig.GetViewConfig(getQueueEmbed)
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

	// GetBaseHandler, will handle the button interactions while the passed handler will execute
	// after updating button state
	handler = paginationConfig.GetBaseHandler(session, handler)
	queueView := views.NewView(*viewConfig, views.WithLogger(logger))

	if err := queueView.SendView(interaction, session, handler); err != nil {
		return fmt.Errorf("sending queue view: %w", err)
	}

	return nil
}

func (g *guildPlayer) generateMusicPlayerView(interaction *discordgo.Interaction, session *discordgo.Session, logger *zap.Logger) error {
	if g.isQueueDepleted() {
		return errEmptyQueue
	}

	viewConfig := g.getMusicPlayerViewConfig()
	musicPlayerView := views.NewView(viewConfig, views.WithLogger(logger))

	handler := func(passedInteraction *discordgo.Interaction) error {
		var actionMessage string
		eg, ctx := errgroup.WithContext(context.Background())

		messageID := passedInteraction.Message.ID
		messageCustomID := passedInteraction.MessageComponentData().CustomID

		switch messageCustomID {
		case "SkipBtn":
			actionMessage = "⏩ ***Track skipped*** 👍"
			g.skip()
			g.sendStopSignal()
		case "PauseResumeBtn":
			if !g.isPaused() {
				if err := g.pause(); err != nil {
					return fmt.Errorf("pausing: %w", err)
				}

				actionMessage = "**Paused** ⏸️"
			} else {
				if err := g.resume(); err != nil {
					return fmt.Errorf("resuming: %w", err)
				}

				actionMessage = "⏯️ **Resuming** 👍"
			}
		case "BackBtn":
			g.rewind()
			g.sendStopSignal()
			actionMessage = "⏪ ***Rewind*** 👍"
		case "ClearBtn":
			g.clearUpcomingTracks()
			actionMessage = "💥 **Cleared...** ⏹"
		case "LikeBtn":
			eg.Go(func() error {
				return g.likeCurrentSong(ctx, passedInteraction.Member.User.ID)
			})
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

		if actionMessage == "" {
			currentSong := g.getCurrentSong()
			if err := util.SendMessage(session, passedInteraction, true, util.MessageData{
				Embeds: embeds.LikedSongEmbed(currentSong),
				FlagWrapper: &util.FlagWrapper{
					Flags: discordgo.MessageFlagsEphemeral,
				},
			}); err != nil {
				return fmt.Errorf("sending liked song message: %w", err)
			}
		} else {
			message, err := session.ChannelMessageSendEmbed(passedInteraction.ChannelID, embeds.MusicPlayerActionEmbed(actionMessage, *interaction.Member))
			if err != nil {
				return fmt.Errorf("sending action initiated message: %w", err)
			}

			if err := util.DeleteMessageAfterTime(session, passedInteraction.ChannelID, message.ID, 30*time.Second); err != nil {
				return fmt.Errorf("deleting message after time: %w", err)
			}

		}

		if err := eg.Wait(); err != nil {
			return fmt.Errorf("liking song: %w", err)
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
	if err := g.view.EditView(viewConfig, session); err != nil {
		return fmt.Errorf("editing view : %w", err)
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

func (g *guildPlayer) getCurrentSong() *audiotype.TrackData {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.queue[g.queuePtr.Load()]
}

func (g *guildPlayer) getCurrentPointer() int {
	return int(g.queuePtr.Load())
}

func (g *guildPlayer) isValidPosition(position int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return position < len(g.queue)
}

// unlike resetQueue, this resets the queue to
// only contain elements up to the queue ptr.
func (g *guildPlayer) clearUpcomingTracks() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = g.queue[g.getCurrentPointer():1]
	g.queuePtr.Store(0)
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

// isBeforeQueuePtr checks if the given index is before the current queue pointer.
// Returns true if the index is less than the position of the current pointer.
func (g *guildPlayer) isBeforeQueuePtr(index int) bool {
	return index < g.getCurrentPointer()
}

func (g *guildPlayer) swap(firstPosition int, secondPosition int) error {
	if !g.isValidPosition(firstPosition) || !g.isValidPosition(secondPosition) || g.isBeforeQueuePtr(firstPosition) || g.isBeforeQueuePtr(secondPosition) {
		return errInvalidPosition
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.queue[firstPosition], g.queue[secondPosition] = g.queue[secondPosition], g.queue[firstPosition]

	return nil
}

func (g *guildPlayer) getTrackAtPosition(position int) *audiotype.TrackData {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.queue[position]
}

func (g *guildPlayer) isPaused() bool {
	return g.voiceState == paused
}

func (g *guildPlayer) isNotActive() bool {
	return g.voiceState == notPlaying
}

func (g *guildPlayer) addTracks(data ...*audiotype.TrackData) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.queue = append(g.queue, data...)
}

func (g *guildPlayer) hasNext() bool {
	return int(g.queuePtr.Load())+1 < len(g.queue)
}

// A queue length of 1 is considered an empty queue
// because the first index is the current song playing.
func (g *guildPlayer) isEmptyQueue() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.queue) <= 2
}

// isQueueDepleted returns true when a guildPlayers queue
// has no elements in it.
func (g *guildPlayer) isQueueDepleted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.queue) == 0
}

func (g *guildPlayer) shuffleQueue() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i := g.getCurrentPointer() + 1; i < len(g.queue); i++ {
		j := rand.Intn(i-g.getCurrentPointer()) + g.getCurrentPointer() + 1
		g.queue[i], g.queue[j] = g.queue[j], g.queue[i]
	}
}

func (g *guildPlayer) pause() error {
	if g.stream == nil {
		return errStreamNonExistent
	}

	g.stream.SetPaused(true)
	g.voiceState = paused

	return nil
}

// Returns the amount of tracks left in the queue based on the
// current queue ptr
func (g *guildPlayer) remainingQueueLength() int {
	if g.isQueueDepleted() {
		return 0
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.queue) - (g.getCurrentPointer()) - 1
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
