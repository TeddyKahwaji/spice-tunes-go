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
