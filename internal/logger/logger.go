package logger

import (
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func GuildID(guildID string) zapcore.Field {
	return zap.String("guild_id", guildID)
}

func UserID(userID string) zapcore.Field {
	return zap.String("user_id", userID)
}

func ChannelID(channelID string) zapcore.Field {
	return zap.String("channel_id", channelID)
}

func NewLogger() *zap.Logger {
	opts := []zap.Option{
		zap.WithCaller(true),
		zap.AddStacktrace(zap.FatalLevel),
	}

	if util.IsProd() {
		return zap.Must(zap.NewProduction(opts...))
	}

	return zap.Must(zap.NewDevelopment(opts...))
}
