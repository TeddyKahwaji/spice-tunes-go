package util

import (
	"time"

	"github.com/bwmarrin/discordgo"
)

func DeleteMessageAfterTime(session *discordgo.Session, channelID string, messageID string, timeDelay time.Duration) error {
	message, err := session.ChannelMessage(channelID, messageID)
	if err != nil {
		return err
	}

	time.AfterFunc(timeDelay, func() {
		_ = session.ChannelMessageDelete(channelID, message.ID)
	})

	return nil
}
