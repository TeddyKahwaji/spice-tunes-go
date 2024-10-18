package spotify

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"github.com/zmb3/spotify"
)

type SpotifyClientWrapper struct {
	client *spotify.Client
}

func NewSpotifyClientWrapper(client *spotify.Client) *SpotifyClientWrapper {
	return &SpotifyClientWrapper{
		client: client,
	}
}

func (s *SpotifyClientWrapper) GetRecommendation(ctx context.Context, query string, limit int) ([]*audiotype.TrackData, error) {
	audioData, err := s.SearchTrack(query)
	if err != nil {
		return nil, fmt.Errorf("search track: %w", err)
	}

	seeds := spotify.Seeds{
		Tracks: []spotify.ID{spotify.ID(audioData.ID)},
	}

	options := spotify.Options{Limit: &limit}
	recommendations, err := s.client.GetRecommendations(seeds, nil, &options)
	if err != nil {
		return nil, fmt.Errorf("getting recommendations: %w", err)
	}

	const requesterNameKey = audiotype.ContextKey("requesterName")

	requesterName, ok := ctx.Value(requesterNameKey).(string)
	if !ok {
		return nil, errors.New("context does not have proper authorization")
	}

	recommendationTrackIDs := funcs.Map(recommendations.Tracks, func(simpleTrack spotify.SimpleTrack) spotify.ID {
		return simpleTrack.ID
	})

	fullTracks, err := s.client.GetTracks(recommendationTrackIDs...)
	if err != nil {
		return nil, fmt.Errorf("error getting full tracks: %w", err)
	}

	result := make([]*audiotype.TrackData, 0, len(recommendations.Tracks))
	for _, track := range fullTracks {
		trackTitle := track.Name + " - " + track.Artists[0].Name
		trackData := &audiotype.TrackData{
			ID:        track.ID.String(),
			TrackName: trackTitle,
			Query:     "ytsearch1:" + trackTitle,
			Requester: requesterName,
			Duration:  track.TimeDuration(),
		}

		if len(track.Album.Images) > 0 {
			trackData.TrackImageURL = track.Album.Images[0].URL
		}

		result = append(result, trackData)
	}

	return result, nil
}

// Search track returns a spotifyID if a matching song was found on spotify.
func (s *SpotifyClientWrapper) SearchTrack(query string) (*audiotype.Data, error) {
	result, err := s.client.Search(query, spotify.SearchTypeTrack)
	if err != nil {
		return nil, fmt.Errorf("searching track: %w", err)
	}

	if result.Tracks == nil || len(result.Tracks.Tracks) == 0 {
		return nil, audiotype.ErrSearchQueryNotFound
	}

	track := result.Tracks.Tracks[0]

	audioData := &audiotype.Data{
		Type: audiotype.SpotifyTrack,
		ID:   track.ID.String(),
	}

	fullTrackName := track.Name + " - " + track.Artists[0].Name
	trackData := &audiotype.TrackData{
		ID:        track.ID.String(),
		TrackName: fullTrackName,
		Duration:  track.TimeDuration(),
		Query:     "ytsearch1:" + fullTrackName,
	}

	if len(track.Album.Images) > 0 {
		trackData.TrackImageURL = track.Album.Images[0].URL
	}

	audioData.Tracks = []*audiotype.TrackData{trackData}

	return audioData, nil
}

func (s *SpotifyClientWrapper) GetTracksData(ctx context.Context, audioType audiotype.SupportedAudioType, query string) (*audiotype.Data, error) {
	var (
		result *audiotype.Data
		err    error
	)

	const requesterNameKey = audiotype.ContextKey("requesterName")

	requesterName, ok := ctx.Value(requesterNameKey).(string)
	if !ok {
		return nil, errors.New("context does not have proper authorization")
	}

	if audioType != audiotype.SpotifyPlaylist && audioType != audiotype.SpotifyTrack && audioType != audiotype.SpotifyAlbum {
		return nil, errors.New("audio type provided is not from a spotify source")
	}

	spotifyTrackID, err := extractSpotifyID(audioType, query)
	if err != nil {
		return nil, fmt.Errorf("extracting playlistID: %w", err)
	}

	switch audioType {
	case audiotype.SpotifyPlaylist:
		result, err = s.handlePlaylistData(requesterName, spotifyTrackID)

	case audiotype.SpotifyAlbum:
		result, err = s.handleAlbumData(requesterName, spotifyTrackID)

	case audiotype.SpotifyTrack:
		result, err = s.handleSingleTrackData(requesterName, spotifyTrackID)
	}

	if err != nil {
		return nil, fmt.Errorf("error getting track data: %w", err)
	}

	if len(result.Tracks) == 0 {
		return nil, audiotype.ErrSearchQueryNotFound
	}

	return result, err
}

func (s *SpotifyClientWrapper) getPlaylistMetaData(spotifyTrackID string) (*audiotype.PlaylistData, error) {
	data, err := s.client.GetPlaylistOpt(spotify.ID(spotifyTrackID), "name,images")
	if err != nil {
		return nil, fmt.Errorf("getting playlist metadata: %w", err)
	}

	result := &audiotype.PlaylistData{
		PlaylistName: data.Name,
	}

	if len(data.Images) > 0 {
		result.PlaylistImageURL = data.Images[0].URL
	}

	return result, nil
}

func (s *SpotifyClientWrapper) handleSingleTrackData(requesterName string, spotifyTrackID string) (*audiotype.Data, error) {
	trackData := make([]*audiotype.TrackData, 0, 1)

	track, err := s.client.GetTrack(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting track data: %w", err)
	}

	trackTitle := track.Name + " - " + track.Artists[0].Name
	trackData = append(trackData, &audiotype.TrackData{
		TrackName:     trackTitle,
		ID:            track.ID.String(),
		TrackImageURL: track.Album.Images[0].URL,
		Query:         "ytsearch1:" + trackTitle,
		Requester:     requesterName,
		Duration:      track.TimeDuration(),
	})

	return &audiotype.Data{
		Tracks: trackData,
		Type:   audiotype.SpotifyTrack,
		ID:     spotifyTrackID,
	}, nil
}

func (s *SpotifyClientWrapper) handleAlbumData(requesterName string, spotifyTrackID string) (*audiotype.Data, error) {
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	trackData := []*audiotype.TrackData{}
	result, err := s.client.GetAlbum(spotify.ID(spotifyTrackID))
	if err != nil {
		return nil, fmt.Errorf("getting album track items: %w", err)
	}

	playlistData := &audiotype.PlaylistData{
		PlaylistName: result.Name,
	}

	if len(result.Images) > 0 {
		playlistData.PlaylistImageURL = result.Images[0].URL
	}

	orderedData := make(map[int][]*audiotype.TrackData)

	page := 0
	for ; ; page++ {
		tracks := result.Tracks.Tracks
		wg.Add(1)

		go func(tracks []spotify.SimpleTrack) {
			defer wg.Done()

			data := make([]*audiotype.TrackData, 0, len(tracks))
			for _, track := range tracks {
				fullTrackName := track.Name + " - " + track.Artists[0].Name
				data = append(data, &audiotype.TrackData{
					TrackName:     track.Name + " - " + track.Artists[0].Name,
					TrackImageURL: result.Images[0].URL,
					Query:         "ytsearch1:" + fullTrackName,
					Requester:     requesterName,
					ID:            track.ID.String(),
					Duration:      track.TimeDuration(),
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

	return &audiotype.Data{
		Tracks:       trackData,
		Type:         audiotype.SpotifyAlbum,
		PlaylistData: playlistData,
		ID:           spotifyTrackID,
	}, nil
}

func (s *SpotifyClientWrapper) handlePlaylistData(requesterName string, spotifyTrackID string) (*audiotype.Data, error) {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		offset int
		limit  = 50
	)

	playlistData, err := s.getPlaylistMetaData(spotifyTrackID)
	if err != nil {
		return nil, err
	}

	trackData := []*audiotype.TrackData{}
	result, err := s.client.GetPlaylistTracksOpt(spotify.ID(spotifyTrackID), &spotify.Options{
		Offset: &offset,
		Limit:  &limit,
	}, "items(track(name,href,album,id,artists,duration_ms(name,href,images))), next")
	if err != nil {
		return nil, fmt.Errorf("getting spotify playlist items: %w", err)
	}

	orderedData := make(map[int][]*audiotype.TrackData)

	page := 0
	for ; ; page++ {
		tracks := result.Tracks
		wg.Add(1)

		go func(tracks []spotify.PlaylistTrack) {
			defer wg.Done()

			data := make([]*audiotype.TrackData, 0, len(tracks))
			for _, track := range tracks {
				fullTrackName := track.Track.Name + " - " + track.Track.Artists[0].Name
				data = append(data, &audiotype.TrackData{
					TrackName:     fullTrackName,
					TrackImageURL: track.Track.Album.Images[0].URL,
					Query:         "ytsearch1:" + fullTrackName,
					Requester:     requesterName,
					ID:            track.Track.ID.String(),
					Duration:      track.Track.TimeDuration(),
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

	return &audiotype.Data{
		Tracks:       trackData,
		Type:         audiotype.SpotifyPlaylist,
		PlaylistData: playlistData,
		ID:           spotifyTrackID,
	}, nil
}

func extractSpotifyID(audioType audiotype.SupportedAudioType, spotifyURL string) (string, error) {
	switch audioType {
	case audiotype.SpotifyPlaylist:
		matches := audiotype.SpotifyPlaylistRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case audiotype.SpotifyAlbum:
		matches := audiotype.SpotifyAlbumRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}

	case audiotype.SpotifyTrack:
		matches := audiotype.SpotifyTrackRegex.FindStringSubmatch(spotifyURL)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", errors.New("error could not extract any ID")
}
