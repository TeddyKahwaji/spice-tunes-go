package audiotype

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

type SupportedAudioType string

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

const (
	YoutubeVideoRegex    = `^((?:https?:)?\/\/)?((?:www|m)\.)?((?:youtube(-nocookie)?\.com|youtu.be))(\/(?:[\w\-]+\?v=|embed\/|v\/)?)([\w\-]+)(\S+)?$`
	YoutubePlaylistRegex = `^.*(youtu.be\/|list=)([^#\&\?]*).*`
	SpotifyAlbumRegex    = `https:\/\/open\.spotify\.com\/album\/([a-zA-Z0-9]+)`
	SpotifyPlaylistRegex = `https:\/\/open\.spotify\.com\/playlist\/([a-zA-Z0-9]+)`
	SpotifyTrackRegex    = `https:\/\/open\.spotify\.com\/track\/([a-zA-Z0-9]+)`
	SoundCloudRegex      = `^https?:\/\/(soundcloud\.com|snd\.sc)\/(.*)$`
)

var (
	ErrUnsupportedAudioType = errors.New("search query provided is not a supported audio type")
	ErrSearchQueryNotFound  = errors.New("search query could not be resolved")
)

func DetermineAudioType(query string) (SupportedAudioType, error) {
	if matched, _ := regexp.MatchString(YoutubePlaylistRegex, query); matched {
		return YoutubePlaylist, nil
	} else if matched, _ := regexp.MatchString(YoutubeVideoRegex, query); matched {
		return YoutubeSong, nil
	} else if matched, _ := regexp.MatchString(SpotifyPlaylistRegex, query); matched {
		return SpotifyPlaylist, nil
	} else if matched, _ := regexp.MatchString(SpotifyAlbumRegex, query); matched {
		return SpotifyAlbum, nil
	} else if matched, _ := regexp.MatchString(SpotifyTrackRegex, query); matched {
		return SpotifyTrack, nil
	} else if matched, _ := regexp.MatchString(SoundCloudRegex, query); matched {
		if regexp.MustCompile(`sets`).MatchString(query) {
			return SoundCloudPlaylist, nil
		}

		return SoundCloudTrack, nil
	}

	// if input is not a URL assume it is a generic search.
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
