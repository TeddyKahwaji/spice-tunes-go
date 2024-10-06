package embeds

import "github.com/bwmarrin/discordgo"

type MusicPlayButtonsConfig struct {
	ForwardDisabled bool
	BackDisabled    bool
	SkipDisabled    bool
	ClearDisabled   bool
	Resume          bool
}

func GetMusicPlayerButtons(config MusicPlayButtonsConfig) []discordgo.MessageComponent {
	pauseResumeBtn := discordgo.Button{
		Disabled: false,
		CustomID: "PauseResumeBtn",
		Label:    "Pause",
		Style:    discordgo.SecondaryButton,
		Emoji: &discordgo.ComponentEmoji{
			Name: "⏸", // Pause emoji
		},
	}

	if config.Resume {
		pauseResumeBtn.Label = "Resume"
		pauseResumeBtn.Emoji.Name = "▶️"
	}

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
				pauseResumeBtn,
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
					Style:    discordgo.DangerButton,
					Emoji: &discordgo.ComponentEmoji{
						Name: "🗑️", // Wastebasket emoji
					},
				},
			},
		},
	}
}
