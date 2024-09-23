package embeds

import "github.com/bwmarrin/discordgo"

func GetMusicPlayerButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.Button{
			Label: "Back",
			Style: discordgo.SecondaryButton,
			Emoji: &discordgo.ComponentEmoji{
				ID: ":rewind:",
			},
		},
		discordgo.Button{
			Label: "Pause",
			Style: discordgo.SecondaryButton,
			Emoji: &discordgo.ComponentEmoji{
				ID: ":pause:",
			},
		},
		discordgo.Button{
			Label: "Skip",
			Style: discordgo.SecondaryButton,
			Emoji: &discordgo.ComponentEmoji{
				ID: ":skip:",
			},
		},
		discordgo.Button{
			Label: "Clear",
			Style: discordgo.SecondaryButton,
			Emoji: &discordgo.ComponentEmoji{
				ID: ":wastebasket:",
			},
		},
	}
}
