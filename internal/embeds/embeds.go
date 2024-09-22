package embeds

import "github.com/bwmarrin/discordgo"

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
