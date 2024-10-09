package music

import "github.com/bwmarrin/discordgo"

var musicPlayerCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "play",
		Description: "Plays desired song/playlist",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "query",
				Description: "Song/playlist search query",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
		},
	},
	{
		Name:        "skip",
		Description: "Skips the current track playing",
	},
	{
		Name:        "pause",
		Description: "Pauses the current track playing",
	},
	{
		Name:        "resume",
		Description: "Resume the current track",
	},
	{
		Name:        "rewind",
		Description: "Rewinds to the previous track in the queue",
	},
	{
		Name:        "clear",
		Description: "Clears the entire music queue",
	},
	{
		Name:        "shuffle",
		Description: "Shuffles the music queue",
	},
	{
		Name:        "swap",
		Description: "Swap the position of two tracks in the queue",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "first_position",
				Description: "The position of the first track in the queue",
				Type:        discordgo.ApplicationCommandOptionInteger,
				Required:    true,
			},
			{
				Name:        "second_position",
				Description: "The position of the second track in the queue",
				Type:        discordgo.ApplicationCommandOptionInteger,
				Required:    true,
			},
		},
	},
}
