package embeds

import (
	"fmt"

	"tunes/pkg/audiotype"

	"github.com/bwmarrin/discordgo"
)

func ErrorMessageEmbed(msg string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "❌ **Invalid usage**",
		Description: msg,
		Color:       0x992D22,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://media.giphy.com/media/S5tkhUBHTTWh865paS/giphy.gif",
		},
	}
}

func NotFoundEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Search Query Has No Results",
		Description: "Sorry, I couldn't find any results for your search.\n\nPlease provide a direct `YouTube` or `Spotify` link.",
		Color:       0xd5a7b4,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://media.giphy.com/media/piL4e4WusrA4S0KODK/giphy.gif",
		},
	}
}

func MusicPlayerEmbed(trackData audiotype.TrackData) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:       "Now Playing 🎵",
		Description: trackData.TrackName,
		Color:       0xd5a7b4,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "`Length:`",
				Value:  audiotype.FormatDuration(trackData.Duration),
				Inline: true,
			},
			{
				Name:   "`Requested by:`",
				Value:  trackData.Requester,
				Inline: true,
			},
		},
	}

	return embed
}

func UnexpectedErrorEmbed() *discordgo.MessageEmbed {
	const supportServerInvite = "https://discord.gg/WsKwCTpKhH"
	const botGif = "https://media.giphy.com/media/v1.Y2lkPTc5MGI3NjExZnphaG5wOG5ldjNwaG5qcmt5M3VubWwzY2RkOXVkeWx5cDNha2Y5YyZlcD12MV9naWZzX3NlYXJjaCZjdD1n/Qbm1Oget7e3vVl9uPB/giphy.gif"

	return &discordgo.MessageEmbed{
		Title:       "Sorry I could not process your request 🤖 🔥",
		Description: fmt.Sprintf("`-` Sorry an unexpected error occurred\n\n`-` If this continues to happen please join the [support channel](%s)", supportServerInvite),
		Color:       0xd333ff,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: botGif,
		},
	}
}
