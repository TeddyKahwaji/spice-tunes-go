package util

import (
	"fmt"
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

func GetVoiceChannelMemberCount(session *discordgo.Session, guildID, channelID string) (int, error) {
	guild, err := session.State.Guild(guildID)
	if err != nil {
		return 0, fmt.Errorf("getting guild: %w", err)
	}

	memberCount := 0

	// Loop through VoiceStates to find all members in the specific voice channel
	for _, vs := range guild.VoiceStates {
		if vs == nil || session == nil || session.State == nil {
			continue
		}

		if vs.ChannelID == channelID && (!vs.Member.User.Bot || vs.UserID == session.State.User.ID) {
			memberCount++
		}
	}

	return memberCount, nil
}

type sendMessageOption struct {
	deletion      bool
	deletionTimer time.Duration
	channelID     string
}

type SendMessageOpt func(*sendMessageOption)

type FlagWrapper struct {
	Flags discordgo.MessageFlags
}
type MessageData struct {
	Embeds      *discordgo.MessageEmbed
	FlagWrapper *FlagWrapper
}

func WithDeletion(deletionTimer time.Duration, channelID string) SendMessageOpt {
	return func(opt *sendMessageOption) {
		opt.deletionTimer = deletionTimer
		opt.deletion = true
		opt.channelID = channelID
	}
}

func SendMessage(session *discordgo.Session, interaction *discordgo.Interaction, isFollowUp bool, msgData MessageData, opts ...SendMessageOpt) error {
	sendMessageOptions := sendMessageOption{}
	for _, opt := range opts {
		opt(&sendMessageOptions)
	}

	if isFollowUp {
		params := &discordgo.WebhookParams{
			Embeds: []*discordgo.MessageEmbed{msgData.Embeds},
		}

		if msgData.FlagWrapper != nil {
			params.Flags = msgData.FlagWrapper.Flags
		}

		_, err := session.FollowupMessageCreate(interaction, false, params)
		if err != nil {
			return fmt.Errorf("sending follow up message: %w", err)
		}
	} else {
		params := &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{msgData.Embeds},
		}

		if msgData.FlagWrapper != nil {
			params.Flags = msgData.FlagWrapper.Flags
		}

		err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: params,
		})
		if err != nil {
			return fmt.Errorf("sending interaction response: %w", err)
		}
	}

	if sendMessageOptions.deletion {
		message, err := session.InteractionResponse(interaction)
		if err != nil {
			return fmt.Errorf("retrieving messageID from interaction response: %w", err)
		}
		if err := DeleteMessageAfterTime(session, sendMessageOptions.channelID, message.ID, sendMessageOptions.deletionTimer); err != nil {
			return fmt.Errorf("deleting message: %w", err)
		}
	}

	return nil
}
