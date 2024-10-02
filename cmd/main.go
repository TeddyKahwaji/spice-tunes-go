package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"tunes/internal/gcp"
	"tunes/internal/music"
	sw "tunes/pkg/spotify"
	"tunes/pkg/youtube"

	"github.com/bwmarrin/discordgo"
	"github.com/zmb3/spotify"
	"go.uber.org/zap"
	"golang.org/x/oauth2/clientcredentials"
)

func getLogger(env string) *zap.Logger {
	if strings.ToUpper(env) == "PROD" {
		return zap.Must(zap.NewProduction(zap.WithCaller(true)))
	}

	return zap.Must(zap.NewDevelopment())
}

func newDiscordBotClient(token string, httpClient *http.Client) (*discordgo.Session, error) {
	bot, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("creating bot: %w", err)
	}

	bot.Client = httpClient

	return bot, nil
}

func newSpotifyWrapperClient(ctx context.Context, clientID string, clientSecret string) *sw.SpotifyWrapper {
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     spotify.TokenURL,
	}

	tokenClient := config.Client(ctx)
	client := spotify.NewClient(tokenClient)

	return sw.NewSpotifyWrapper(&client)
}

func main() {
	env := os.Getenv("ENV")
	logger := getLogger(env)

	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Warn("could not sync logger", zap.Error(err))
		}
	}()

	discordToken := os.Getenv("SPICE_TUNES_DISCORD_TOKEN")
	httpTimeout := time.Second * 5

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

	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
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

		musicCogConfig := &music.CogConfig{
			HttpClient:           &httpClient,
			SpotifyWrapper:       spotifyWrapper,
			Logger:               logger,
			YoutubeSearchWrapper: youtubeSearchWrapper,
		}

		musicPlayerCog, err := music.NewMusicPlayerCog(musicCogConfig)
		if err != nil {
			logger.Fatal("unable to instantiate greeter cog", zap.Error(err))
		}

		if err = musicPlayerCog.RegisterCommands(session); err != nil {
			logger.Fatal("unable to register greeter commands", zap.Error(err))
		}

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
