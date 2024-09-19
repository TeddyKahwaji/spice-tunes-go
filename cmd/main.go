package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
	"tunes/internal/music"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

func getLogger(env string) *zap.Logger {
	if strings.ToUpper(env) == "PROD" {
		return zap.Must(zap.NewProduction(zap.WithCaller(true)))
	} else {
		return zap.Must(zap.NewDevelopment())
	}
}

func newDiscordBotClient(token string, httpClient *http.Client) (*discordgo.Session, error) {
	bot, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("creating bot: %w", err)
	}

	bot.Client = httpClient

	return bot, nil
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

	httpClient := http.Client{
		Timeout: time.Second * 5,
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

	bot.AddHandler(func(session *discordgo.Session, _ *discordgo.Ready) {
		musicPlayerCog, err := music.NewMusicPlayerCog(logger, &httpClient)
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
