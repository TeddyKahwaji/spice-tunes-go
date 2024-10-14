package audiotype

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

type (
	SupportedAudioType string
	ContextKey         string
)

type TrackData struct {
	TrackName     string        `firestore:"track_name"`
	TrackImageURL string        `firestore:"track_image_url"`
	Query         string        `firestore:"query"`
	Requester     string        `firestore:"requester"`
	Duration      time.Duration `firestore:"duration"`
	ID            string        `firestore:"ID"`
}
type PlaylistData struct {
	PlaylistName     string `firestore:"playlist_name"`
	PlaylistImageURL string `firestore:"playlist_image_url"`
}

type Data struct {
	Tracks       []*TrackData       `firestore:"track_data"`
	Type         SupportedAudioType `firestore:"supported_audio_type"`
	PlaylistData *PlaylistData      `firestore:"playlist_data,omitempty"`
	ID           string             `firestore:"ID"`
}

const (
	YoutubeSong        SupportedAudioType = "YoutubeSongAudio"
	YoutubePlaylist    SupportedAudioType = "YoutubePlaylistAudio"
	SpotifyTrack       SupportedAudioType = "SpotifyTrackAudio"
	SpotifyPlaylist    SupportedAudioType = "SpotifyPlaylistAudio"
	SpotifyAlbum       SupportedAudioType = "SpotifyAlbumAudio"
	SoundCloudTrack    SupportedAudioType = "SoundCloud"
	SoundCloudPlaylist SupportedAudioType = "SoundCloudPlaylistAudio"
	GenericSearch      SupportedAudioType = "GenericSearchAudio"
)

var (
	YoutubeVideoRegex    = regexp.MustCompile(`^(?:https?:\/\/)?(?:www\.)?(?:youtube\.com\/(?:[^\/\n\s]+\/\S+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([a-zA-Z0-9_-]{11})(?:\S+)?$`)
	YoutubePlaylistRegex = regexp.MustCompile(`[?&]list=([a-zA-Z0-9_-]+)`)
	SpotifyAlbumRegex    = regexp.MustCompile(`https:\/\/open\.spotify\.com\/album\/([a-zA-Z0-9]+)`)
	SpotifyPlaylistRegex = regexp.MustCompile(`https:\/\/open\.spotify\.com\/playlist\/([a-zA-Z0-9]+)`)
	SpotifyTrackRegex    = regexp.MustCompile(`https:\/\/open\.spotify\.com\/track\/([a-zA-Z0-9]+)`)
	SoundCloudRegex      = regexp.MustCompile(`^https?:\/\/(soundcloud\.com|snd\.sc)\/(.*)$`)
	SoundCloudSetsRegex  = regexp.MustCompile(`sets`)
)

var (
	ErrUnsupportedAudioType = errors.New("search query provided is not a supported audio type")
	ErrSearchQueryNotFound  = errors.New("search query could not be resolved")
)

func DetermineAudioType(query string) (SupportedAudioType, error) {
	// YouTube Video (prioritize video ID first)
	if YoutubeVideoRegex.MatchString(query) {
		return YoutubeSong, nil
	}

	// YouTube Playlist (only match if list= is in the URL)
	if YoutubePlaylistRegex.MatchString(query) {
		return YoutubePlaylist, nil
	}

	// Spotify Playlist
	if SpotifyPlaylistRegex.MatchString(query) {
		return SpotifyPlaylist, nil
	}

	// Spotify Album
	if SpotifyAlbumRegex.MatchString(query) {
		return SpotifyAlbum, nil
	}

	// Spotify Track
	if SpotifyTrackRegex.MatchString(query) {
		return SpotifyTrack, nil
	}

	// SoundCloud
	if SoundCloudRegex.MatchString(query) {
		if SoundCloudSetsRegex.MatchString(query) {
			return SoundCloudPlaylist, nil
		}
		return SoundCloudTrack, nil
	}

	// Assume it is a generic search if the input is not a URL
	if u, err := url.Parse(query); err != nil || u.Scheme == "" || u.Host == "" {
		return GenericSearch, nil
	}

	return "", ErrUnsupportedAudioType
}

// audio type is a playlist.
func IsMultiTrackType(audioType SupportedAudioType) bool {
	return audioType == SpotifyPlaylist || audioType == SpotifyAlbum
}

func IsSpotify(audioType SupportedAudioType) bool {
	return audioType == SpotifyAlbum || audioType == SpotifyPlaylist || audioType == SpotifyTrack
}

func IsYoutube(audioType SupportedAudioType) bool {
	return audioType == YoutubePlaylist || audioType == YoutubeSong
}

func FormatDuration(time time.Duration) string {
	if time.Hours() >= 1 {
		return fmt.Sprintf("%02d:%02d:%02d", int(time.Hours()), int(time.Minutes())%60, int(time.Seconds())%60)
	}
	return fmt.Sprintf("%02d:%02d", int(time.Minutes()), int(time.Seconds())%60)
}
