package commands

import "github.com/bwmarrin/discordgo"

type ApplicationCommandHandler func(*discordgo.Session, *discordgo.InteractionCreate) error

type ApplicationCommand struct {
	CommandConfiguration *discordgo.ApplicationCommand
	Handler              ApplicationCommandHandler
}
