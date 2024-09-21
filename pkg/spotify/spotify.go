package spotify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
	music "tunes/internal/music"

	"github.com/zmb3/spotify"
)

type SpotifyWrapper struct {
	client *spotify.Client
}

type TrackData struct {
	TrackName     string
	TrackImageURL string
	TrackDuration time.Duration
}

type SpotifyData struct {
	Tracks []TrackData
	Type   music.SupportedAudioType
}

func (s *SpotifyWrapper) GetTracksData(ctx context.Context, query string) (*SpotifyData, error) {
	var (
		result *SpotifyData
		err    error
	)

	audioType, err := music.DetermineAudioType(query)
	if err != nil {
		return nil, fmt.Errorf("determining audio type: %w", err)
	} else if audioType != music.SpotifyPlaylist && audioType != music.SpotifyTrack && audioType != music.SpotifyAlbum {
		return nil, errors.New("audio type provided is not from a spotify source")
	}

	spotifyTrackID, err := extractSpotifyID(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("extracting playlistID: %w", err)
	}

	switch audioType {
	case music.SpotifyPlaylist:
		result, err = s.handlePlaylistData(spotifyTrackID)

	case music.SpotifyAlbum:
		result, err = s.handleAlbumData(spotifyTrackID)

	case music.SpotifyTrack:
		result, err = s.handleSingleTrackData(spotifyTrackID)
	}

	if err != nil {
		return nil, fmt.Errorf("error getting track data: %w", err)
	}
	return result, err
}

func (s *SpotifyWrapper) handleSingleTrackData(spotifyTrackID string) (*SpotifyData, error) {
	trackData := make([]TrackData, 1)

	track, err := s.client.GetTrack(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	trackData = append(trackData, TrackData{
		TrackName:     track.Name,
		TrackImageURL: track.Album.Images[0].URL,
		TrackDuration: track.TimeDuration(),
	})

	return &SpotifyData{
		Tracks: trackData,
		Type:   music.SpotifyTrack,
	}, nil
}

func (s *SpotifyWrapper) handleAlbumData(spotifyTrackID string) (*SpotifyData, error) {
	trackData := []TrackData{}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	result, err := s.client.GetAlbum(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting album track items: %w", err)
	}

	for {
		tracks := result.Tracks.Tracks
		wg.Add(1)

		go func(tracks []spotify.SimpleTrack) {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			for _, track := range tracks {
				trackData = append(trackData, TrackData{
					TrackName:     track.Name + " - " + track.Artists[0].Name,
					TrackImageURL: result.Images[0].URL,
					TrackDuration: track.TimeDuration(),
				})
			}

		}(tracks)

		if result.Tracks.Next == "" {
			wg.Wait()
			break
		}

		if err = s.client.NextPage(&result.Tracks); err != nil {
			return nil, fmt.Errorf("getting next page: %w", err)
		}

	}

	return &SpotifyData{
		Tracks: trackData,
		Type:   music.SpotifyAlbum,
	}, nil

}
func (s *SpotifyWrapper) handlePlaylistData(spotifyTrackID string) (*SpotifyData, error) {
	trackData := []TrackData{}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		offset int = 0
		limit  int = 50
	)

	result, err := s.client.GetPlaylistTracksOpt(spotify.ID(spotifyTrackID), &spotify.Options{
		Offset: &offset,
		Limit:  &limit,
	}, "items(track(name,href,album,artists,duration_ms(name,href,images))), next")

	if err != nil {
		return nil, fmt.Errorf("getting spotify playlist items: %w", err)
	}

	for {
		tracks := result.Tracks
		wg.Add(1)

		go func(tracks []spotify.PlaylistTrack) {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			for _, track := range tracks {
				trackData = append(trackData, TrackData{
					TrackName:     track.Track.Name + " - " + track.Track.Artists[0].Name,
					TrackImageURL: track.Track.Album.Images[0].URL,
					TrackDuration: track.Track.TimeDuration(),
				})
			}

		}(tracks)

		if result.Next == "" {
			wg.Wait()
			break
		}

		if err = s.client.NextPage(result); err != nil {
			return nil, fmt.Errorf("getting next page: %w", err)
		}

	}

	return &SpotifyData{
		Tracks: trackData,
		Type:   music.SpotifyPlaylist,
	}, nil
}

func extractSpotifyID(audioType music.SupportedAudioType, spotifyURL string) (string, error) {
	playlistRegex := regexp.MustCompile(music.SpotifyPlaylistRegex)
	singleTrackRegex := regexp.MustCompile(music.SpotifyTrackRegex)
	albumRegex := regexp.MustCompile(music.SpotifyAlbumRegex)

	switch audioType {
	case music.SpotifyPlaylist:
		matches := playlistRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case music.SpotifyAlbum:
		matches := albumRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case music.SpotifyTrack:
		matches := singleTrackRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	}

	return "", errors.New("error could not find playlistID")
}
