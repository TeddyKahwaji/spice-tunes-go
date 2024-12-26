package music

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/pagination"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/views"
	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type voiceState string

const (
	playing    voiceState = "PLAYING"
	paused     voiceState = "PAUSED"
	notPlaying voiceState = "NOT_PLAYING"
)

const (
	guildCollection    string = "Guilds"
	userDataCollection string = "UserData"
	likedTracksPath    string = "LikedTracks"
)

const (
	paginationSeparator int = 8
)

var (
	errStreamNonExistent = errors.New("no stream exists")
	errUserHasNoLikes    = errors.New("user has no likes")
	errNoViews           = errors.New("guild player does not have any views")
	errEmptyQueue        = errors.New("queue is empty")
	errInvalidPosition   = errors.New("position provided is out of bounds")
)

type userData struct {
	LikedTracks []*audiotype.TrackData `firestore:"LikedTracks"`
}

type supportedView string

var (
	musicPlayer supportedView = "MusicPlayerView"
	queue       supportedView = "QueuePlayerView"
)

type guildView struct {
	view     *views.View
	viewType supportedView
}

type guildPlayer struct {
	guildID         string
	channelID       string
	mu              sync.RWMutex
	logger          *zap.Logger
	voiceClient     *discordgo.VoiceConnection
	queue           []*audiotype.TrackData
	voiceState      voiceState
	queuePtr        atomic.Int32
	stream          *dca.StreamingSession
	doneChannel     chan error
	stopChannel     chan bool
	views           map[*guildView]struct{}
	fireStoreClient FireStore
}

func newGuildPlayer(vc *discordgo.VoiceConnection, channelID string, fireStoreClient FireStore, logger *zap.Logger) *guildPlayer {
	return &guildPlayer{
		voiceClient:     vc,
		guildID:         vc.GuildID,
		channelID:       channelID,
		queue:           make([]*audiotype.TrackData, 0),
		views:           make(map[*guildView]struct{}),
		logger:          logger,
		voiceState:      notPlaying,
		stopChannel:     make(chan bool),
		fireStoreClient: fireStoreClient,
	}
}

func (g *guildPlayer) hasView() bool {
	return len(g.views) > 0
}

func (g *guildPlayer) getMusicPlayerViewConfig() *views.Config {
	if g.queuePtr.Load() >= int32(len(g.queue)) {
		g.queuePtr.Store(int32(len(g.queue) - 1))
	}

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

	return &views.Config{
		Components: &views.ComponentHandler{
			MessageComponents: musicPlayerButtons,
		},
		Embeds: []*discordgo.MessageEmbed{musicPlayerEmbed},
	}
}

// This is a best case effort, if the song doesn't exist we don't like but don't propagate an error to the user
// this will only return errors in non-404 case.
func (g *guildPlayer) likeCurrentSong(ctx context.Context, session *discordgo.Session, interaction *discordgo.Interaction) error {
	userID := interaction.Member.User.ID
	currentSong := g.getCurrentSong()

	docRef, err := g.fireStoreClient.GetDocumentFromCollection(ctx, guildCollection, g.guildID).
		Collection(userDataCollection).
		Doc(userID).
		Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			if _, err := docRef.Ref.Set(ctx, userData{
				LikedTracks: []*audiotype.TrackData{currentSong},
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
			Value: firestore.ArrayUnion(currentSong),
		},
	}); err != nil {
		return fmt.Errorf("updating document: %w", err)
	}

	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
	}); err != nil {
		return fmt.Errorf("sending update message: %w", err)
	}

	if err := util.SendMessage(session, interaction, true, util.MessageData{
		Embeds: embeds.LikedSongEmbed(currentSong),
		FlagWrapper: &util.FlagWrapper{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		return fmt.Errorf("sending liked song message: %w", err)
	}

	return nil
}

func (g *guildPlayer) getQueuePaginationConfig() (*pagination.PaginationConfig[audiotype.TrackData], error) {
	if g.getCurrentPointer()+1 >= len(g.queue) {
		return nil, errInvalidPosition
	}

	paginationConfig := pagination.NewPaginatedConfig(g.queue[g.getCurrentPointer()+1:], paginationSeparator)

	return paginationConfig, nil
}

func (g *guildPlayer) generateMusicQueueView(interaction *discordgo.Interaction, session *discordgo.Session) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	guild, err := util.GetGuild(session, interaction.GuildID)
	if err != nil {
		return fmt.Errorf("getting guild: %w", err)
	}

	paginationConfig, err := g.getQueuePaginationConfig()
	if err != nil {
		return fmt.Errorf("getting queue view config: %w", err)
	}

	getQueueEmbed := func(tracks []*audiotype.TrackData, pageNumber int, totalPages int, separator int) *discordgo.MessageEmbed {
		return embeds.QueueEmbed(tracks, pageNumber, totalPages, separator, guild)
	}

	viewConfig := paginationConfig.GetViewConfig(getQueueEmbed)

	handler := func(passedInteraction *discordgo.Interaction) error {
		messageID := passedInteraction.Message.ID
		paginationConfig.UpdateData(g.queue[g.getCurrentPointer()+1:], paginationSeparator)

		viewConfig := paginationConfig.GetViewConfig(getQueueEmbed)
		_, err = session.ChannelMessageEditComplex(&discordgo.MessageEdit{
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
	queueView := views.NewView(viewConfig, views.WithLogger(g.logger), views.WithDeletion(5*time.Minute))

	if err := queueView.SendView(interaction, session, handler); err != nil {
		return fmt.Errorf("sending queue view: %w", err)
	}

	createdView := &guildView{
		view:     queueView,
		viewType: queue,
	}
	g.views[createdView] = struct{}{}

	return nil
}

func (g *guildPlayer) generateMusicPlayerView(interaction *discordgo.Interaction, session *discordgo.Session) error {
	if g.isQueueDepleted() {
		return errEmptyQueue
	}

	viewConfig := g.getMusicPlayerViewConfig()
	musicPlayerView := views.NewView(viewConfig, views.WithLogger(g.logger))

	handler := func(passedInteraction *discordgo.Interaction) error {
		var actionMessage string
		eg, ctx := errgroup.WithContext(context.Background())
		messageID := passedInteraction.Message.ID
		messageCustomID := passedInteraction.MessageComponentData().CustomID

		switch messageCustomID {
		case "SkipBtn":
			actionMessage = "⏩ **Track Skipped** 👍"
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
			actionMessage = "⏪ **Rewind** 👍"
		case "ClearBtn":
			g.clearUpcomingTracks()
			actionMessage = "💥 **Cleared...** ⏹"
		case "LikeBtn":
			return g.likeCurrentSong(ctx, session, passedInteraction)
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

		eg.Go(func() error {
			return g.refreshState(session)
		})

		if err := session.InteractionRespond(passedInteraction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
		}); err != nil {
			return fmt.Errorf("sending update message: %w", err)
		}

		message, err := session.ChannelMessageSendEmbed(passedInteraction.ChannelID, embeds.MusicPlayerActionEmbed(actionMessage, *interaction.Member))
		if err != nil {
			return fmt.Errorf("sending action initiated message: %w", err)
		}

		if err := util.DeleteMessageAfterTime(session, passedInteraction.ChannelID, message.ID, 30*time.Second); err != nil {
			return fmt.Errorf("deleting message after time: %w", err)
		}

		if err := eg.Wait(); err != nil {
			return fmt.Errorf("liking song: %w", err)
		}

		return nil
	}

	if err := musicPlayerView.SendView(interaction, session, handler); err != nil {
		return fmt.Errorf("sending music player view: %w", err)
	}

	createdView := &guildView{
		view:     musicPlayerView,
		viewType: musicPlayer,
	}

	g.views[createdView] = struct{}{}

	return nil
}

func (g *guildPlayer) getLikes(ctx context.Context, userID string) ([]*audiotype.TrackData, error) {
	docRef, err := g.fireStoreClient.GetDocumentFromCollection(ctx, guildCollection, g.guildID).
		Collection(userDataCollection).
		Doc(userID).
		Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, errUserHasNoLikes
		}
		return nil, fmt.Errorf("getting document: %w", err)
	}

	var userData userData
	if err := docRef.DataTo(&userData); err != nil {
		return nil, fmt.Errorf("converting data to userData struct: %w", err)
	}

	filter := map[string]*audiotype.TrackData{}

	for _, track := range userData.LikedTracks {
		filter[track.TrackName] = track
	}

	return slices.Collect(maps.Values(filter)), nil
}

func (g *guildPlayer) refreshState(session *discordgo.Session) error {
	if len(g.views) == 0 {
		return errNoViews
	}

	guild, err := util.GetGuild(session, g.guildID)
	if err != nil {
		return fmt.Errorf("getting guild: %w", err)
	}

	var deleteQueueView bool

	musicViewConfig := g.getMusicPlayerViewConfig()

	queueViewConfig, err := g.getQueuePaginationConfig()
	if err != nil {
		deleteQueueView = true
	}

	getQueueEmbed := func(tracks []*audiotype.TrackData, pageNumber int, totalPages int, separator int) *discordgo.MessageEmbed {
		return embeds.QueueEmbed(tracks, pageNumber, separator, totalPages, guild)
	}

	for guildView := range g.views {
		if guildView.viewType == musicPlayer {
			if err := guildView.view.EditView(musicViewConfig, session); err != nil {
				g.logger.Warn("unable to refresh music player view", zap.Error(err))
				delete(g.views, guildView)
			}
		} else {
			if deleteQueueView {
				_ = guildView.view.DeleteView(session)
				delete(g.views, guildView)

				continue
			}

			viewConfig := queueViewConfig.GetViewConfig(getQueueEmbed)

			if err := guildView.view.EditView(viewConfig, session); err != nil {
				g.logger.Warn("unable to refresh queue view", zap.Error(err))
				delete(g.views, guildView)
			}
		}
	}

	return nil
}

func (g *guildPlayer) destroyAllViews(session *discordgo.Session) {
	if len(g.views) == 0 {
		return
	}

	for guildView := range g.views {
		if err := guildView.view.DeleteView(session); err != nil {
			g.logger.Warn("unable to delete view", zap.Error(err))
		}
	}

	g.views = map[*guildView]struct{}{}
}

func (g *guildPlayer) getCurrentSong() *audiotype.TrackData {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.queue[g.queuePtr.Load()]
}

func (g *guildPlayer) getCurrentPointer() int {
	return int(g.queuePtr.Load())
}

func (g *guildPlayer) removeTrack(position int) (*audiotype.TrackData, error) {
	if !g.isValidPosition(position) || position == 0 {
		return nil, errInvalidPosition
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	track := g.queue[position]
	g.queue = append(g.queue[:position], g.queue[position+1:]...)

	return track, nil
}

func (g *guildPlayer) isValidPosition(position int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return position >= 0 && position < len(g.queue)
}

// unlike resetQueue, this resets the queue to
// only contain the current track playing
func (g *guildPlayer) clearUpcomingTracks() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = g.queue[:1]
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
	isInvalidPositions := !g.isValidPosition(firstPosition) || !g.isValidPosition(secondPosition)
	isBeforeQueuePtr := g.isBeforeQueuePtr(firstPosition) || g.isBeforeQueuePtr(secondPosition)
	isZeroIndex := firstPosition == 0 || secondPosition == 0 // 0 index is reserved for the track currently playing only, which cannot be swapped.

	if isInvalidPositions || isBeforeQueuePtr || isZeroIndex {
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
	g.mu.Lock()
	defer g.mu.Unlock()

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
	g.mu.Lock()
	defer g.mu.Unlock()

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
