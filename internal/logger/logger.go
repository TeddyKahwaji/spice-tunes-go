package logger

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func GuildID(guildID string) zapcore.Field {
	return zap.String("guild_id", guildID)
}

func ChannelID(channelID string) zapcore.Field {
	return zap.String("channel_id", channelID)
}

func NewLogger(env string) *zap.Logger {
	if strings.ToUpper(env) == "PROD" {
		return zap.Must(zap.NewProduction(zap.WithCaller(true)))
	}

	return zap.Must(zap.NewDevelopment())
}
