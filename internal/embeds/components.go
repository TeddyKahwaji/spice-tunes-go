package embeds

import "github.com/bwmarrin/discordgo"

type MusicPlayButtonsConfig struct {
	ForwardDisabled bool
	BackDisabled    bool
	PauseDisabled   bool
	SkipDisabled    bool
	ClearDisabled   bool
}

func GetMusicPlayerButtons(config MusicPlayButtonsConfig) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Disabled: config.BackDisabled,
					CustomID: "BackBtn",
					Label:    "Back",
					Style:    discordgo.SecondaryButton,
					Emoji: &discordgo.ComponentEmoji{
						Name: "⏮", // Rewind emoji
					},
				},
				discordgo.Button{
					Disabled: config.PauseDisabled,
					CustomID: "PauseBtn",
					Label:    "Pause",
					Style:    discordgo.SecondaryButton,
					Emoji: &discordgo.ComponentEmoji{
						Name: "⏸", // Pause emoji
					},
				},
				discordgo.Button{
					Disabled: config.SkipDisabled,
					CustomID: "SkipBtn",
					Label:    "Skip",
					Style:    discordgo.SecondaryButton,
					Emoji: &discordgo.ComponentEmoji{
						Name: "⏭", // Skip emoji
					},
				},
				discordgo.Button{
					Disabled: config.ClearDisabled,
					CustomID: "ClearBtn",
					Label:    "Clear",
					Style:    discordgo.SecondaryButton,
					Emoji: &discordgo.ComponentEmoji{
						Name: "🗑️", // Wastebasket emoji
					},
				},
			},
		},
	}
}
