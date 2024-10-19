package audit

import (
	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

const (
	guildAuditLogChannelID = "1094732412845576268"
)

func (a *AuditCog) guildDeleteEvent(session *discordgo.Session, guildDeleteEvent *discordgo.GuildDelete) {
	if util.IsProd() {
		_, err := session.ChannelMessageSendEmbed(guildAuditLogChannelID, embeds.GuildAuditEmbed(guildDeleteEvent.BeforeDelete, false))
		if err != nil {
			a.logger.Warn("unable to send guild audit delete event", zap.Error(err), logger.GuildID(guildDeleteEvent.ID))
		}

		delete(a.alreadyJoinedGuilds, guildDeleteEvent.ID)
	}
}

func (a *AuditCog) guildJoinedEvent(session *discordgo.Session, guildJoinedEvent *discordgo.GuildCreate) {
	if util.IsProd() {
		if guildJoinedEvent.Unavailable {
			return
		}

		if _, exists := a.alreadyJoinedGuilds[guildJoinedEvent.Guild.ID]; exists {
			return
		}

		_, err := session.ChannelMessageSendEmbed(guildAuditLogChannelID, embeds.GuildAuditEmbed(guildJoinedEvent.Guild, true))
		if err != nil {
			a.logger.Warn("unable to send guild audit joined event", zap.Error(err), logger.GuildID(guildJoinedEvent.Guild.ID))
		}

		a.alreadyJoinedGuilds[guildJoinedEvent.Guild.ID] = struct{}{}
	}
}
