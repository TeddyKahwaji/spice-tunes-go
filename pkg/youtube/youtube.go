package youtube

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"tunes/pkg/audiotype"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	YoutubeVideoBase = "https://www.youtube.com/watch?v="
)

type YoutubeSearchWrapper struct {
	ytPlaylistService *youtube.PlaylistItemsService
	ytVideoService    *youtube.VideosService
	ytSearchService   *youtube.SearchService
}

func NewYoutubeSearchWrapper(ctx context.Context, apiKey string) (*YoutubeSearchWrapper, error) {
	service, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("instantiating new service: %w", err)
	}

	ytPlaylistService := youtube.NewPlaylistItemsService(service)
	ytVideoService := youtube.NewVideosService(service)
	ytSearchService := youtube.NewSearchService(service)

	return &YoutubeSearchWrapper{
		ytPlaylistService: ytPlaylistService,
		ytVideoService:    ytVideoService,
		ytSearchService:   ytSearchService,
	}, nil
}

func extractYoutubeID(audioType audiotype.SupportedAudioType, fullURL string) (string, error) {
	playlistRegex := regexp.MustCompile(`[?&]list=([a-zA-Z0-9_-]+)`)
	singleVideoRegex := regexp.MustCompile(`(?:youtube\.com\/(?:[^\/\n\s]+\/\S+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^?&\n]{11})`)

	switch audioType {
	case audiotype.YoutubePlaylist:
		matches := playlistRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	case audiotype.YoutubeSong:
		matches := singleVideoRegex.FindStringSubmatch(fullURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", errors.New("error could not extract any ID")
}

func (yt *YoutubeSearchWrapper) GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error) {
	var (
		trackData *audiotype.Data
		err       error
	)

	if ctxData := ctx.Value("requesterName"); ctxData == nil {
		return nil, errors.New("context must contain requesterName, otherwise not authorized to get track data")
	}

	requesterName := ctx.Value("requesterName").(string)

	if audioType == audiotype.GenericSearch {
		trackData, err = yt.handleGenericSearch(requesterName, query)
	} else {
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
	}

	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	return trackData, nil
}

func (yt *YoutubeSearchWrapper) handleSingleTrack(requesterName string, ID string) (*audiotype.Data, error) {
	resp, err := yt.ytVideoService.List([]string{"snippet", "contentDetails"}).
		Id(ID).
		MaxResults(1).
		Do()
	if err != nil {
		return nil, fmt.Errorf("requesting single video: %w", err)
	}

	trackData := make([]audiotype.TrackData, 0, 1)

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

	trackData = append(trackData, audiotype.TrackData{
		TrackImageURL: thumbnailURL,
		TrackName:     item.Snippet.Title,
		Query:         YoutubeVideoBase + ID,
		Requester:     requesterName,
	})

	return &audiotype.Data{
		Tracks: trackData,
		Type:   audiotype.YoutubeSong,
	}, nil
}

func (yt *YoutubeSearchWrapper) handleGenericSearch(requesterName string, query string) (*audiotype.Data, error) {
	resp, err := yt.ytSearchService.List([]string{"snippet"}).
		Q(query).
		MaxResults(1).
		Do()
	if err != nil {
		return nil, fmt.Errorf("searching youtube query: %w", err)
	}

	if len(resp.Items) == 0 {
		return nil, audiotype.ErrSearchQueryNotFound
	}

	trackData := make([]audiotype.TrackData, 0, 1)

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

	trackData = append(trackData, audiotype.TrackData{
		TrackImageURL: thumbnailURL,
		TrackName:     item.Snippet.Title,
		Query:         YoutubeVideoBase + item.Id.VideoId,
		Requester:     requesterName,
	})

	return &audiotype.Data{
		Tracks: trackData,
		Type:   audiotype.GenericSearch,
	}, nil
}

func (yt *YoutubeSearchWrapper) handlePlaylist(requesterName string, ID string) (*audiotype.Data, error) {
	req := yt.ytPlaylistService.List([]string{"snippet", "contentDetails"}).
		PlaylistId(ID).
		MaxResults(100)

	trackData := []audiotype.TrackData{}

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
			return nil, audiotype.ErrSearchQueryNotFound
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, item := range items {
				data := audiotype.TrackData{
					Requester: requesterName,
				}

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

	return &audiotype.Data{
		Tracks: trackData,
		Type:   audiotype.YoutubePlaylist,
	}, nil
}
