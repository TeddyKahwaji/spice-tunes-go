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

	switch audioType {
	case models.YoutubePlaylist:
		matches := playlistRegex.FindStringSubmatch(fullURL)
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
	return nil, nil
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

		wg.Add(1)
		items := resp.Items
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

				fullURL := "https://www.youtube.com/watch?v=" + videoID
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
