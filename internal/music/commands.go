package music

import "github.com/bwmarrin/discordgo"

type applicationCommandHandler func(*discordgo.Session, *discordgo.InteractionCreate) error

type applicationCommand struct {
	commandConfiguration *discordgo.ApplicationCommand
	handler              applicationCommandHandler
}

func (m *playerCog) getApplicationCommands() map[string]*applicationCommand {
	return map[string]*applicationCommand{
		"play": {
			handler: m.play,
			commandConfiguration: &discordgo.ApplicationCommand{
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
		},
		"pause": {
			handler: m.pause,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "pause",
				Description: "Pauses the current track playing",
			},
		},
		"resume": {
			handler: m.resume,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "resume",
				Description: "Resume the current track",
			},
		},
		"skip": {
			handler: m.skip,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "skip",
				Description: "Skips the current track playing",
			},
		},
		"rewind": {
			handler: m.rewind,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "rewind",
				Description: "Rewinds to the previous track in the queue",
			},
		},
		"swap": {
			handler: m.swap,
			commandConfiguration: &discordgo.ApplicationCommand{
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
		},
		"shuffle": {
			handler: m.shuffle,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "shuffle",
				Description: "Shuffles the music queue",
			},
		},
		"clear": {
			handler: m.clear,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "clear",
				Description: "Clears the entire music queue",
			},
		},
		"queue": {
			handler: m.queue,
			commandConfiguration: &discordgo.ApplicationCommand{
				Name:        "queue",
				Description: "Displays the music queue",
			},
		},
	}
}
