package spotify

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	models "tunes/pkg/models"

	"github.com/zmb3/spotify"
)

type SpotifyWrapper struct {
	client *spotify.Client
}

func NewSpotifyWrapper(client *spotify.Client) *SpotifyWrapper {
	return &SpotifyWrapper{
		client: client,
	}
}

func (s *SpotifyWrapper) GetTracksData(query string) (*models.Data, error) {
	var (
		result *models.Data
		err    error
	)

	audioType, err := models.DetermineAudioType(query)
	if err != nil {
		return nil, fmt.Errorf("determining audio type: %w", err)
	} else if audioType != models.SpotifyPlaylist && audioType != models.SpotifyTrack && audioType != models.SpotifyAlbum {
		return nil, errors.New("audio type provided is not from a spotify source")
	}

	spotifyTrackID, err := extractSpotifyID(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("extracting playlistID: %w", err)
	}

	switch audioType {
	case models.SpotifyPlaylist:
		result, err = s.handlePlaylistData(spotifyTrackID)

	case models.SpotifyAlbum:
		result, err = s.handleAlbumData(spotifyTrackID)

	case models.SpotifyTrack:
		result, err = s.handleSingleTrackData(spotifyTrackID)
	}

	if err != nil {
		return nil, fmt.Errorf("error getting track data: %w", err)
	}
	return result, err
}

func (s *SpotifyWrapper) handleSingleTrackData(spotifyTrackID string) (*models.Data, error) {
	trackData := make([]models.TrackData, 1)

	track, err := s.client.GetTrack(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	trackData = append(trackData, models.TrackData{
		TrackName:     track.Name,
		TrackImageURL: track.Album.Images[0].URL,
		TrackDuration: track.TimeDuration(),
		Query:         "ytsearch1:" + track.Name,
	})

	return &models.Data{
		Tracks: trackData,
		Type:   models.SpotifyTrack,
	}, nil
}

func (s *SpotifyWrapper) handleAlbumData(spotifyTrackID string) (*models.Data, error) {
	trackData := []models.TrackData{}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	result, err := s.client.GetAlbum(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting album track items: %w", err)
	}

	orderedData := make(map[int][]models.TrackData)

	page := 0
	for ; ; page++ {
		tracks := result.Tracks.Tracks

		wg.Add(1)
		go func(tracks []spotify.SimpleTrack) {
			defer wg.Done()

			data := make([]models.TrackData, 0, len(tracks))
			for _, track := range tracks {
				fullTrackName := track.Name + " - " + track.Artists[0].Name
				data = append(data, models.TrackData{
					TrackName:     track.Name + " - " + track.Artists[0].Name,
					TrackImageURL: result.Images[0].URL,
					TrackDuration: track.TimeDuration(),
					Query:         "ytsearch1:" + fullTrackName,
				})
			}

			mu.Lock()
			orderedData[page] = data
			mu.Unlock()
		}(tracks)

		if result.Tracks.Next == "" {
			wg.Wait()
			break
		}

		if err = s.client.NextPage(&result.Tracks); err != nil {
			return nil, fmt.Errorf("getting next page: %w", err)
		}

	}

	for currentPage := range page + 1 {
		if data, ok := orderedData[currentPage]; ok {
			trackData = append(trackData, data...)
		}
	}

	return &models.Data{
		Tracks: trackData,
		Type:   models.SpotifyAlbum,
	}, nil
}

func (s *SpotifyWrapper) handlePlaylistData(spotifyTrackID string) (*models.Data, error) {
	trackData := []models.TrackData{}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		offset int
		limit  int = 50
	)

	result, err := s.client.GetPlaylistTracksOpt(spotify.ID(spotifyTrackID), &spotify.Options{
		Offset: &offset,
		Limit:  &limit,
	}, "items(track(name,href,album,artists,duration_ms(name,href,images))), next")
	if err != nil {
		return nil, fmt.Errorf("getting spotify playlist items: %w", err)
	}

	orderedData := make(map[int][]models.TrackData)

	page := 0
	for ; ; page++ {
		tracks := result.Tracks
		wg.Add(1)

		go func(tracks []spotify.PlaylistTrack) {
			defer wg.Done()

			data := make([]models.TrackData, 0, len(tracks))
			for _, track := range tracks {
				fullTrackName := track.Track.Name + " - " + track.Track.Artists[0].Name
				data = append(data, models.TrackData{
					TrackName:     fullTrackName,
					TrackImageURL: track.Track.Album.Images[0].URL,
					TrackDuration: track.Track.TimeDuration(),
					Query:         "ytsearch1:" + fullTrackName,
				})
			}

			mu.Lock()
			orderedData[page] = data
			mu.Unlock()
		}(tracks)

		if result.Next == "" {
			wg.Wait()
			break
		}

		if err = s.client.NextPage(result); err != nil {
			return nil, fmt.Errorf("getting next page: %w", err)
		}

	}

	for currentPage := range page + 1 {
		if data, ok := orderedData[currentPage]; ok {
			trackData = append(trackData, data...)
		}
	}

	return &models.Data{
		Tracks: trackData,
		Type:   models.SpotifyPlaylist,
	}, nil
}

func extractSpotifyID(audioType models.SupportedAudioType, spotifyURL string) (string, error) {
	playlistRegex := regexp.MustCompile(models.SpotifyPlaylistRegex)
	singleTrackRegex := regexp.MustCompile(models.SpotifyTrackRegex)
	albumRegex := regexp.MustCompile(models.SpotifyAlbumRegex)

	switch audioType {
	case models.SpotifyPlaylist:
		matches := playlistRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case models.SpotifyAlbum:
		matches := albumRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case models.SpotifyTrack:
		matches := singleTrackRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	}

	return "", errors.New("error could not find playlistID")
}
