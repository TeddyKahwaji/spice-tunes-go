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
		Name:        "rewind",
		Description: "Rewinds to the previous track in the queue",
	},
	{
		Name:        "clear",
		Description: "Clears the entire music queue",
	},
}
