package audit

import (
	"os"

	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	guildAuditLogChannelID = "1094732412845576268"
)

func (m *AuditCog) guildDeleteEvent(session *discordgo.Session, guildDeleteEvent *discordgo.GuildDelete) {
	if env := os.Getenv("ENV"); env == "PROD" {
		_, err := session.ChannelMessageSendEmbed(guildAuditLogChannelID, embeds.GuildAuditEmbed(guildDeleteEvent.BeforeDelete, false))
		if err != nil {
			m.logger.Warn("unable to send guild audit delete event", zap.Error(err), logger.GuildID(guildDeleteEvent.ID))
		}
	}
}

func (m *AuditCog) guildJoinedEvent(session *discordgo.Session, guildJoinedEvent *discordgo.GuildCreate) {
	if env := os.Getenv("ENV"); env == "PROD" {
		_, err := session.ChannelMessageSendEmbed(guildAuditLogChannelID, embeds.GuildAuditEmbed(guildJoinedEvent.Guild, true))
		if err != nil {
			m.logger.Warn("unable to send guild audit joined event", zap.Error(err), logger.GuildID(guildJoinedEvent.Guild.ID))
		}
	}
}
