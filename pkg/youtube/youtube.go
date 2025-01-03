package youtube

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	YoutubeVideoBase = "https://www.youtube.com/watch?v="
)

type SearchWrapper struct {
	ytPlaylistItemsService *youtube.PlaylistItemsService
	ytVideoService         *youtube.VideosService
	ytSearchService        *youtube.SearchService
	ytPlaylistService      *youtube.PlaylistsService
}

func NewYoutubeSearchWrapper(ctx context.Context, creds []byte) (*SearchWrapper, error) {
	service, err := youtube.NewService(ctx, option.WithCredentialsJSON(creds))
	if err != nil {
		return nil, fmt.Errorf("instantiating new service: %w", err)
	}

	return &SearchWrapper{
		ytPlaylistItemsService: youtube.NewPlaylistItemsService(service),
		ytVideoService:         youtube.NewVideosService(service),
		ytSearchService:        youtube.NewSearchService(service),
		ytPlaylistService:      youtube.NewPlaylistsService(service),
	}, nil
}

func extractYoutubeID(audioType audiotype.SupportedAudioType, fullURL string) (string, error) {
	switch audioType {
	case audiotype.YoutubeSong:
		matches := audiotype.YoutubeVideoRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case audiotype.YoutubePlaylist:
		matches := audiotype.YoutubePlaylistRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", errors.New("error: could not extract any ID")
}

func (yt *SearchWrapper) GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error) {
	var (
		trackData *audiotype.Data
		err       error
	)
	const requesterNameKey = audiotype.ContextKey("requesterName")

	requesterName, ok := ctx.Value(requesterNameKey).(string)
	if !ok {
		return nil, errors.New("context does not have proper authorization")
	}

	if audioType == audiotype.GenericSearch {
		if trackData, err = yt.handleGenericSearch(requesterName, query); err != nil {
			return nil, fmt.Errorf("getting generic search data: %w", err)
		}

		return trackData, nil
	}

	youtubeID, err := extractYoutubeID(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("extracting youtube ID: %w", err)
	}

	switch audioType {
	case audiotype.YoutubePlaylist:
		trackData, err = yt.handlePlaylist(requesterName, youtubeID)
	case audiotype.YoutubeSong:
		trackData, err = yt.handleSingleTrack(requesterName, youtubeID)
	}

	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	return trackData, nil
}

func (yt *SearchWrapper) handleSingleTrack(requesterName string, ID string) (*audiotype.Data, error) {
	resp, err := yt.ytVideoService.List([]string{"snippet", "contentDetails"}).
		Id(ID).
		MaxResults(1).
		Do()

	if err != nil {
		return nil, fmt.Errorf("requesting single video: %w", err)
	}

	trackData := make([]*audiotype.TrackData, 0, 1)

	if len(resp.Items) == 0 {
		return nil, audiotype.ErrSearchQueryNotFound
	}

	item := resp.Items[0]

	var thumbnailURL string

	if thumbnails := item.Snippet.Thumbnails; thumbnails != nil {
		if maxRes := thumbnails.Maxres; maxRes != nil {
			thumbnailURL = maxRes.Url
		} else if highRes := thumbnails.High; highRes != nil {
			thumbnailURL = highRes.Url
		}
	}

	duration, err := parseISO8601Duration(item.ContentDetails.Duration)
	if err != nil {
		return nil, fmt.Errorf("retrieving duration of video: %w", err)
	}

	trackData = append(trackData, &audiotype.TrackData{
		TrackImageURL: thumbnailURL,
		TrackName:     item.Snippet.Title,
		Query:         YoutubeVideoBase + ID,
		Requester:     requesterName,
		Duration:      duration,
		ID:            ID,
	})

	return &audiotype.Data{
		Tracks: trackData,
		Type:   audiotype.YoutubeSong,
		ID:     ID,
	}, nil
}

func (yt *SearchWrapper) handleGenericSearch(requesterName string, query string) (*audiotype.Data, error) {
	resp, err := yt.ytSearchService.List([]string{"snippet"}).
		Q(query).
		Type("video").
		EventType("none").
		MaxResults(1).
		Do()

	if err != nil {
		return nil, fmt.Errorf("searching youtube query: %w", err)
	}

	if len(resp.Items) == 0 {
		return nil, audiotype.ErrSearchQueryNotFound
	}

	item := resp.Items[0]

	return yt.handleSingleTrack(requesterName, item.Id.VideoId)
}

func (yt *SearchWrapper) getPlaylistMetaData(ID string) (*audiotype.PlaylistData, error) {
	req := yt.ytPlaylistService.List([]string{"snippet", "contentDetails"}).Id(ID)

	resp, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("getting playlist meta data: %w", err)
	}

	if len(resp.Items) == 0 {
		return nil, errors.New("unable to get playlist metadata")
	}

	result := &audiotype.PlaylistData{
		PlaylistName: resp.Items[0].Snippet.Title,
	}

	if thumbnail := resp.Items[0].Snippet.Thumbnails; thumbnail != nil {
		result.PlaylistImageURL = thumbnail.High.Url
	}

	return result, nil
}

func (yt *SearchWrapper) handlePlaylist(requesterName string, ID string) (*audiotype.Data, error) {
	req := yt.ytPlaylistItemsService.List([]string{"snippet", "contentDetails"}).
		PlaylistId(ID).
		MaxResults(100)

	trackData := []*audiotype.TrackData{}

	playlistMetaData, err := yt.getPlaylistMetaData(ID)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	eg, _ := errgroup.WithContext(context.Background())

	for {
		resp, err := req.Do()
		if err != nil {
			return nil, fmt.Errorf("requesting playlist page: %w", err)
		}

		items := resp.Items
		if len(items) == 0 {
			return nil, audiotype.ErrSearchQueryNotFound
		}

		eg.Go(func() error {
			ids := funcs.Map(items, func(playlistItem *youtube.PlaylistItem) string {
				return playlistItem.ContentDetails.VideoId
			})

			videos, err := yt.ytVideoService.List([]string{"snippet", "contentDetails"}).
				Id(strings.Join(ids, ",")).
				Do()
			if err != nil {
				return fmt.Errorf("listing video ids: %w", err)
			}

			for _, item := range videos.Items {
				data := &audiotype.TrackData{
					Requester: requesterName,
					ID:        ID,
				}

				if thumbnails := item.Snippet.Thumbnails; thumbnails != nil {
					var thumbnailURL string
					if thumbnails.Maxres != nil {
						thumbnailURL = thumbnails.Maxres.Url
					} else if thumbnails.High != nil {
						thumbnailURL = thumbnails.High.Url
					}

					data.TrackImageURL = thumbnailURL
				}

				videoID := item.Id
				fullURL := YoutubeVideoBase + videoID

				title := item.Snippet.Title
				duration, err := parseISO8601Duration(item.ContentDetails.Duration)
				if err != nil {
					return fmt.Errorf("retrieving duration of video: %w", err)
				}

				data.Duration = duration
				data.TrackName = title
				data.Query = fullURL

				mu.Lock()
				trackData = append(trackData, data)
				mu.Unlock()
			}

			return nil
		})

		if resp.NextPageToken == "" {
			break
		}

		req = req.PageToken(resp.NextPageToken)
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("retrieving items from youtube playlist: %w", err)
	}

	return &audiotype.Data{
		Tracks:       trackData,
		Type:         audiotype.YoutubePlaylist,
		PlaylistData: playlistMetaData,
		ID:           ID,
	}, nil
}

func parseISO8601Duration(isoDuration string) (time.Duration, error) {
	re := regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)

	matches := re.FindStringSubmatch(isoDuration)
	if len(matches) == 0 {
		return 0, errors.New("invalid ISO 8601 duration format")
	}

	var hours, minutes, seconds int

	if matches[1] != "" {
		hours, _ = strconv.Atoi(matches[1])
	}

	if matches[2] != "" {
		minutes, _ = strconv.Atoi(matches[2])
	}

	if matches[3] != "" {
		seconds, _ = strconv.Atoi(matches[3])
	}

	duration := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second

	return duration, nil
}
