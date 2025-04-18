package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	fb "firebase.google.com/go"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/audit"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/firebase"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/gcp"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/music"
	sw "github.com/TeddyKahwaji/spice-tunes-go/pkg/spotify"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/youtube"
	"github.com/bwmarrin/discordgo"
	"github.com/zmb3/spotify"
	"go.uber.org/zap"
	"golang.org/x/oauth2/clientcredentials"
	"google.golang.org/api/option"
)

type Cog interface {
	RegisterCommands(*discordgo.Session)
}

var (
	_ Cog = (*music.PlayerCog)(nil)
	_ Cog = (*audit.AuditCog)(nil)
)

func newDiscordBotClient(token string, httpClient *http.Client) (*discordgo.Session, error) {
	bot, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("creating bot: %w", err)
	}

	bot.Client = httpClient

	return bot, nil
}

func newSpotifyWrapperClient(ctx context.Context, clientID string, clientSecret string) *sw.SpotifyClientWrapper {
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     spotify.TokenURL,
	}

	tokenClient := config.Client(ctx)
	client := spotify.NewClient(tokenClient)

	return sw.NewSpotifyClientWrapper(&client)
}

func newFirebaseClient(ctx context.Context, projectID string) (*firebase.Client, error) {
	creds, err := gcp.GetCredentials()
	if err != nil {
		return nil, fmt.Errorf("getting gcp credentials  %w", err)
	}

	app, err := fb.NewApp(ctx, &fb.Config{ProjectID: projectID}, option.WithCredentialsJSON(creds))
	if err != nil {
		return nil, fmt.Errorf("creating new firebase client %w", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating firestore client %w", err)
	}

	return firebase.NewClient(fsClient), nil
}

func main() {
	discordToken := os.Getenv("DISCORD_TOKEN")
	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")

	logger := logger.NewLogger()
	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Warn("could not sync logger", zap.Error(err))
		}
	}()

	httpTimeout := time.Second * 30
	httpClient := http.Client{
		Timeout: httpTimeout,
	}

	bot, err := newDiscordBotClient(discordToken, &httpClient)
	if err != nil {
		logger.Fatal("bot could not be booted", zap.Error(err))
	}

	bot.Identify.Intents = discordgo.IntentsGuildMembers | discordgo.IntentsAllWithoutPrivileged
	bot.StateEnabled = true
	bot.Identify.Presence = discordgo.GatewayStatusUpdate{
		Game: discordgo.Activity{
			Name: "/help",
			Type: discordgo.ActivityTypeGame,
		},
	}

	const gcpProjectID = "dj-bot-46e53"
	creds, err := gcp.GetCredentials()
	if err != nil {
		logger.Fatal("unable to retrieve gcp credentials", zap.Error(err))
	}

	bot.AddHandler(func(session *discordgo.Session, _ *discordgo.Ready) {
		ctx := context.Background()

		spotifyWrapper := newSpotifyWrapperClient(ctx, clientID, clientSecret)

		youtubeSearchWrapper, err := youtube.NewYoutubeSearchWrapper(ctx, creds)
		if err != nil {
			logger.Fatal("unable to instantiate youtubeWrapperClient", zap.Error(err))
		}

		firebaseClient, err := newFirebaseClient(ctx, gcpProjectID)
		if err != nil {
			logger.Fatal("unable to instantiate firebase client", zap.Error(err))
		}

		musicPlayerCog, err := music.NewPlayerCog(&music.CogConfig{
			FireStoreClient:      firebaseClient,
			Session:              session,
			HTTPClient:           &httpClient,
			SpotifyWrapper:       spotifyWrapper,
			Logger:               logger,
			YoutubeSearchWrapper: youtubeSearchWrapper,
		})
		if err != nil {
			logger.Fatal("unable to instantiate music cog", zap.Error(err))
		}

		musicPlayerCog.GlobalPlay()

		musicPlayerCog.RegisterCommands(session)

		auditCog, err := audit.NewAuditCog(&audit.CogConfig{
			Session:    session,
			Logger:     logger,
			HTTPClient: &httpClient,
		})
		if err != nil {
			logger.Fatal("unable to instantiate audit cog", zap.Error(err))
		}

		auditCog.RegisterCommands(session)

		logger.Info("Bot has connected")
	})

	if err := bot.Open(); err != nil {
		logger.Fatal("error opening connection", zap.Error(err))
	}

	defer func() {
		if err := bot.Close(); err != nil {
			logger.Warn("couldn't close bot", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
