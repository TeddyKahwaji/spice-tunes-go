package youtube

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"tunes/pkg/models"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	YoutubeVideoBase = "https://www.youtube.com/watch?v="
)

type YoutubeSearchWrapper struct {
	ytPlaylistService *youtube.PlaylistItemsService
	ytVideoService    *youtube.VideosService
}

func NewYoutubeSearchWrapper(ctx context.Context, apiKey string) (*YoutubeSearchWrapper, error) {
	service, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("instantiating new service: %w", err)
	}

	ytPlaylistService := youtube.NewPlaylistItemsService(service)
	ytVideoService := youtube.NewVideosService(service)

	return &YoutubeSearchWrapper{
		ytPlaylistService: ytPlaylistService,
		ytVideoService:    ytVideoService,
	}, nil
}

func extractYoutubeID(audioType models.SupportedAudioType, fullURL string) (string, error) {
	playlistRegex := regexp.MustCompile(`[?&]list=([a-zA-Z0-9_-]+)`)
	singleVideoRegex := regexp.MustCompile(`(?:youtube\.com\/(?:[^\/\n\s]+\/\S+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^?&\n]{11})`)

	switch audioType {
	case models.YoutubePlaylist:
		matches := playlistRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	case models.YoutubeSong:
		matches := singleVideoRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", errors.New("error could not extract any ID")
}

// TODO: Add context timeouts
func (yt *YoutubeSearchWrapper) GetTracksData(audioType models.SupportedAudioType, query string) (*models.Data, error) {
	var (
		trackData *models.Data
		err       error
	)

	youtubeID, err := extractYoutubeID(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("extracting youtube ID: %w", err)
	}

	switch audioType {
	case models.YoutubePlaylist:
		trackData, err = yt.handlePlaylist(youtubeID)
	case models.YoutubeSong:
		trackData, err = yt.handleSingleTrack(youtubeID)
	}

	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	return trackData, nil
}

func (yt *YoutubeSearchWrapper) handleSingleTrack(ID string) (*models.Data, error) {
	resp, err := yt.ytVideoService.List([]string{"snippet", "contentDetails"}).
		Id(ID).
		MaxResults(1).
		Do()
	if err != nil {
		return nil, fmt.Errorf("requesting single video: %w", err)
	}

	trackData := make([]models.TrackData, 0, 1)

	if len(resp.Items) == 0 {
		return nil, models.ErrSearchQueryNotFound
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

	trackData = append(trackData, models.TrackData{
		TrackImageURL: thumbnailURL,
		TrackName:     item.Snippet.Title,
		Query:         YoutubeVideoBase + ID,
	})

	return &models.Data{
		Tracks: trackData,
		Type:   models.YoutubeSong,
	}, nil
}

func (yt *YoutubeSearchWrapper) handlePlaylist(ID string) (*models.Data, error) {
	req := yt.ytPlaylistService.List([]string{"snippet", "contentDetails"}).
		PlaylistId(ID).
		MaxResults(100)

	trackData := []models.TrackData{}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for {
		resp, err := req.Do()
		if err != nil {
			return nil, fmt.Errorf("requesting playlist page: %w", err)
		}

		items := resp.Items
		if len(items) == 0 {
			return nil, models.ErrSearchQueryNotFound
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, item := range items {
				data := models.TrackData{}
				videoID := item.Snippet.ResourceId.VideoId
				if thumbnails := item.Snippet.Thumbnails; thumbnails != nil {
					var thumbnailURL string
					if thumbnails.Maxres != nil {
						thumbnailURL = thumbnails.Maxres.Url
					} else if thumbnails.High != nil {
						thumbnailURL = thumbnails.High.Url
					}

					data.TrackImageURL = thumbnailURL
				}

				fullURL := YoutubeVideoBase + videoID
				title := item.Snippet.Title
				data.TrackName = title
				data.Query = fullURL

				mu.Lock()
				trackData = append(trackData, data)
				mu.Unlock()

			}
		}()

		if resp.NextPageToken == "" {
			break
		}

		req = req.PageToken(resp.NextPageToken)

	}

	wg.Wait()

	return &models.Data{
		Tracks: trackData,
		Type:   models.YoutubePlaylist,
	}, nil
}
