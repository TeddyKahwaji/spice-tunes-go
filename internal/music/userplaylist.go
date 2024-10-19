package music

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errPlaylistAlreadyExists = errors.New("playlist already exists")
	errNoPlaylistsCreated    = errors.New("user does not have any created playlists")
	errPlaylistDoesNotExist  = errors.New("the provided playlist does not exist")
)

const (
	savedPlaylistsField string = "Playlists"
)

type userPlaylistRetriever struct {
	fireStoreClient FireStore
	playlistCache   map[string]*usersPlaylists
}

type usersPlaylists struct {
	Playlists []*userCreatedPlaylist `firestore:"Playlists"`
}

type userCreatedPlaylist struct {
	Name   string                 `firestore:"Name"`
	Tracks []*audiotype.TrackData `firestore:"Tracks"`
}

func newUserPlaylistRetriever(fs FireStore) *userPlaylistRetriever {
	return &userPlaylistRetriever{
		fireStoreClient: fs,
		playlistCache:   make(map[string]*usersPlaylists),
	}
}

func (u *userPlaylistRetriever) getUserPlaylist(ctx context.Context, userID string, playlistName string) (*userCreatedPlaylist, error) {
	if data, ok := u.playlistCache[userID]; ok {
		for _, playlist := range data.Playlists {
			if strings.EqualFold(playlist.Name, playlistName) {
				return playlist, nil
			}
		}
	}

	playlists, err := u.getUserPlaylists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting user (%s) playlists: %w", userID, err)
	}

	for _, playlist := range playlists.Playlists {
		if playlist.Name == playlistName {
			return playlist, nil
		}
	}

	return nil, errPlaylistDoesNotExist
}

func (u *userPlaylistRetriever) deleteUserPlaylist(ctx context.Context, userID string, playlistName string) error {
	doc, err := u.fireStoreClient.GetDocumentFromCollection(ctx, savedPlaylistsField, userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return errPlaylistDoesNotExist
		}

		return fmt.Errorf("getting user playlist from collection: %w", err)
	}

	var playlists usersPlaylists

	if err := doc.DataTo(&playlists); err != nil {
		return fmt.Errorf("could not transform document data to userPlaylists: %w", err)
	}

	if len(playlists.Playlists) == 0 {
		return errPlaylistDoesNotExist
	}

	found := false
	for i, playlist := range playlists.Playlists {
		if strings.EqualFold(playlist.Name, playlistName) {
			playlists.Playlists = append(playlists.Playlists[:i], playlists.Playlists[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return errPlaylistDoesNotExist
	}

	// Update Firestore with the modified playlists array
	if _, err := doc.Ref.Set(ctx, playlists); err != nil {
		return fmt.Errorf("updating saved playlist document: %w", err)
	}

	return nil
}

func (u *userPlaylistRetriever) updateUserPlaylist(ctx context.Context, userID string, playlistName string, newTracks []*audiotype.TrackData) (*userCreatedPlaylist, error) {
	doc, err := u.fireStoreClient.GetDocumentFromCollection(ctx, savedPlaylistsField, userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, errPlaylistDoesNotExist
		}

		return nil, fmt.Errorf("getting user playlist from collection: %w", err)
	}

	var playlists usersPlaylists

	if err := doc.DataTo(&playlists); err != nil {
		return nil, fmt.Errorf("could not transform document data to userPlaylists: %w", err)
	}

	// Find the target playlist by name
	var targetPlaylist *userCreatedPlaylist
	for _, playlist := range playlists.Playlists {
		if strings.EqualFold(playlist.Name, playlistName) {
			targetPlaylist = playlist
			break
		}
	}

	if targetPlaylist == nil {
		return nil, errPlaylistDoesNotExist
	}

	targetPlaylist.Tracks = append(targetPlaylist.Tracks, newTracks...)

	// Update Firestore with the modified playlists array
	if _, err := doc.Ref.Set(ctx, playlists); err != nil {
		return nil, fmt.Errorf("updating saved playlist document: %w", err)
	}

	return targetPlaylist, nil
}

func (u *userPlaylistRetriever) getUserPlaylists(ctx context.Context, userID string) (*usersPlaylists, error) {
	if playlists, playlistsExist := u.playlistCache[userID]; playlistsExist {
		return playlists, nil
	}

	doc, err := u.fireStoreClient.GetDocumentFromCollection(ctx, savedPlaylistsField, userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// return empty data
			return &usersPlaylists{Playlists: []*userCreatedPlaylist{}}, errNoPlaylistsCreated
		}

		return nil, fmt.Errorf("could not get playlist document for %s: %w", userID, err)
	}

	var playlists usersPlaylists

	if err := doc.DataTo(&playlists); err != nil {
		return nil, fmt.Errorf("could not transform document data to userPlaylists: %w", err)
	}

	for _, playlist := range playlists.Playlists {
		if playlist.Tracks == nil {
			playlist.Tracks = []*audiotype.TrackData{}
		}
	}

	u.playlistCache[userID] = &playlists

	_ = time.AfterFunc(time.Second*30, func() {
		delete(u.playlistCache, userID)
	})

	return &playlists, nil
}

func (u *userPlaylistRetriever) saveUserPlaylist(ctx context.Context, userID string, playlistName string) error {
	// Prepare the new playlist data
	playlistData := &userCreatedPlaylist{
		Name:   playlistName,
		Tracks: []*audiotype.TrackData{},
	}

	docRef, err := u.fireStoreClient.GetDocumentFromCollection(ctx, savedPlaylistsField, userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {

			userPlaylists := usersPlaylists{
				Playlists: []*userCreatedPlaylist{playlistData},
			}

			if _, err := docRef.Ref.Set(ctx, userPlaylists); err != nil {
				return fmt.Errorf("creating saved playlist: %w", err)
			}

			u.playlistCache[userID] = &userPlaylists

			return nil

		}

		return fmt.Errorf("getting document: %w", err)
	}

	var usersPlaylists usersPlaylists
	if err := docRef.DataTo(&usersPlaylists); err != nil {
		return fmt.Errorf("transforming into userPlaylists model: %w", err)
	}

	for _, playlist := range usersPlaylists.Playlists {
		if strings.EqualFold(playlist.Name, playlistName) {
			return errPlaylistAlreadyExists
		}

		if playlist.Tracks == nil {
			playlist.Tracks = []*audiotype.TrackData{}
		}
	}

	u.playlistCache[userID] = &usersPlaylists

	// Update existing document with the new playlist
	if _, err := docRef.Ref.Update(ctx, []firestore.Update{
		{
			Path:  savedPlaylistsField,
			Value: firestore.ArrayUnion(playlistData),
		},
	}); err != nil {
		return fmt.Errorf("updating saved playlist document: %w", err)
	}

	return nil
}
