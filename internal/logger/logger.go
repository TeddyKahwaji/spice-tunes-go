package logger

import (
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func GuildID(guildID string) zapcore.Field {
	return zap.String("guild_id", guildID)
}

func ChannelID(channelID string) zapcore.Field {
	return zap.String("channel_id", channelID)
}

func NewLogger() *zap.Logger {
	if util.IsProd() {
		return zap.Must(zap.NewProduction(zap.WithCaller(true)))
	}

	return zap.Must(zap.NewDevelopment())
}
