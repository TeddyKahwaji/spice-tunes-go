package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/internal/embeds"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/goutubedl"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/logger"
	"github.com/TeddyKahwaji/spice-tunes-go/internal/util"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/commands"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
	"go.uber.org/zap"
)

func (m *PlayerCog) globalPlay() {
	for gp := range m.songSignal {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					m.logger.Warn("recovered from panic that occurred on playAudio", logger.GuildID(gp.guildID))
				}
			}()

			if err := m.playAudio(gp); err != nil {
				m.logger.Warn("error playing audio", logger.GuildID(gp.guildID), zap.Error(err))
			}
		}()
	}
}

func (m *PlayerCog) joinAndCreateGuildPlayer(session *discordgo.Session, interaction *discordgo.InteractionCreate) error {
	voiceState, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		return fmt.Errorf("getting voice state: %w", err)
	}

	if _, ok := m.guildVoiceStates[interaction.GuildID]; !ok {
		channelVoiceConnection, err := session.ChannelVoiceJoin(interaction.GuildID, voiceState.ChannelID, false, true)
		if err != nil {
			return fmt.Errorf("error unable to join voice channel: %w", err)
		}

		guildPlayerLogger := m.logger.With(logger.GuildID(interaction.GuildID))
		m.guildVoiceStates[interaction.GuildID] = newGuildPlayer(channelVoiceConnection, interaction.ChannelID, m.fireStoreClient, guildPlayerLogger)
	}

	return nil
}

func (m *PlayerCog) downloadTrack(ctx context.Context, audioTrackName string) (*os.File, error) {
	userName := "oauth"
	password := ""

	options := goutubedl.Options{
		Type:       goutubedl.TypeSingle,
		HTTPClient: m.httpClient,
		DebugLog:   zap.NewStdLog(m.logger),
		StderrFn:   func(cmd *exec.Cmd) io.Writer { return os.Stderr },
		Username:   &userName,
		Password:   &password,
	}

	downloadOptions := goutubedl.DownloadOptions{
		DownloadAudioOnly: true,
	}

	if strings.Contains(audioTrackName, "ytsearch") {
		options.Type = goutubedl.TypePlaylist
		downloadOptions.PlaylistIndex = 1
	}

	result, err := goutubedl.New(ctx, audioTrackName, options)
	if err != nil {
		return nil, fmt.Errorf("attempting to download from youtube: %w", err)
	}

	downloadResult, err := result.DownloadWithOptions(ctx, downloadOptions)
	if err != nil {
		return nil, fmt.Errorf("downloading youtube data: %w", err)
	}

	defer func() {
		if err := downloadResult.Close(); err != nil {
			m.logger.Warn("couldn't close downloaded result", zap.Error(err))
		}
	}()

	file, err := util.DownloadFileToTempDirectory(downloadResult)
	if err != nil {
		return nil, fmt.Errorf("downloading youtube content to temporary file: %w", err)
	}

	return file, nil
}

func (m *PlayerCog) playAudio(guildPlayer *guildPlayer) error {
	// exit if no voice client or no tracks in the queue
	if guildPlayer == nil || guildPlayer.voiceClient == nil || guildPlayer.isQueueDepleted() {
		return nil
	}

	if guildPlayer.hasView() {
		if err := guildPlayer.refreshState(m.session); err != nil {
			m.logger.Warn("unable to refresh views", zap.Error(err), zap.String("guild_id", guildPlayer.guildID))
		}
	}

	audioTrackQuery := guildPlayer.getCurrentSong().Query

	ctx := context.Background()

	file, err := m.downloadTrack(ctx, audioTrackQuery)
	if err != nil {
		return fmt.Errorf("downloading result: %w", err)
	}

	defer func() {
		if err := util.DeleteFile(file.Name()); err != nil {
			m.logger.Warn("could not delete file", zap.Error(err), zap.String("file_name", file.Name()))
		}
	}()

	opts := dca.StdEncodeOptions
	opts.RawOutput = true
	opts.Bitrate = 120

	encodingStream, err := dca.EncodeFile(file.Name(), opts)
	if err != nil {
		return fmt.Errorf("encoding file: %w", err)
	}

	defer encodingStream.Cleanup()

	guildPlayer.doneChannel = make(chan error)
	guildPlayer.stream = dca.NewStream(encodingStream, guildPlayer.voiceClient, guildPlayer.doneChannel)
	guildPlayer.setVoiceState(playing)

	for {
		select {
		case err := <-guildPlayer.doneChannel:
			if err != nil {
				if errors.Is(err, io.EOF) {
					if guildPlayer.hasNext() {
						guildPlayer.skip()
						m.songSignal <- guildPlayer
					} else {
						guildPlayer.setVoiceState(notPlaying)
						guildPlayer.resetQueue()
						guildPlayer.destroyAllViews(m.session)
					}
				} else {
					m.logger.Warn("error during audio stream", zap.Error(err))
				}
			}

			return nil

		// receiving signal from stop channel indicates queue ptr has shifted
		case <-guildPlayer.stopChannel:
			m.songSignal <- guildPlayer

			return nil
		}
	}
}

func (m *PlayerCog) addToQueue(session *discordgo.Session, interaction *discordgo.InteractionCreate, trackData *audiotype.Data, guildPlayer *guildPlayer) error {
	addedPosition := guildPlayer.remainingQueueLength() + 1
	guildPlayer.addTracks(trackData.Tracks...)

	if guildPlayer.isNotActive() {
		m.songSignal <- guildPlayer
	}

	if guildPlayer.hasView() {
		if err := guildPlayer.refreshState(session); err != nil {
			m.logger.Warn("unable to refresh view state", zap.Error(err), logger.GuildID(interaction.GuildID))
		}

		addedTrackEmbed, err := embeds.AddedTracksEmbed(trackData, interaction.Member, addedPosition)
		if err != nil {
			m.logger.Warn("was not able to provide user with added tracks message embed", zap.Error(err), logger.GuildID(interaction.GuildID))
			return nil
		}

		if err := util.SendMessage(session, interaction.Interaction, true, util.MessageData{
			Embeds: addedTrackEmbed,
		}, util.WithDeletion(30*time.Second, interaction.ChannelID)); err != nil {
			return fmt.Errorf("sending message: %w", err)
		}
	} else {
		if err := guildPlayer.generateMusicPlayerView(interaction.Interaction, session); err != nil {
			m.logger.Error("unable to generate music player view", zap.Error(err), logger.GuildID(interaction.GuildID))
		}
	}

	return nil
}

// Helper function to throw error for commands requiring user to be in voice channel
func (m *PlayerCog) verifyInChannelAndSendError(session *discordgo.Session, interaction *discordgo.InteractionCreate) (bool, error) {
	_, err := session.State.VoiceState(interaction.GuildID, interaction.Member.User.ID)
	if err != nil {
		if errors.Is(err, discordgo.ErrStateNotFound) {
			invalidUsageEmbed := embeds.ErrorMessageEmbed(fmt.Sprintf("%s, you must be in a voice channel.", interaction.Member.User.Username))
			msgData := util.MessageData{
				Embeds: invalidUsageEmbed,
				Type:   discordgo.InteractionResponseChannelMessageWithSource,
				FlagWrapper: &util.FlagWrapper{
					Flags: discordgo.MessageFlagsEphemeral,
				},
			}

			err := util.SendMessage(session, interaction.Interaction, false, msgData)
			if err != nil {
				return false, fmt.Errorf("interaction response: %w", err)
			}

			return false, nil
		}

		return false, fmt.Errorf("retrieving voice state: %w", err)
	}

	return true, nil
}

func (m *PlayerCog) reportErrorToSupportChannel(session *discordgo.Session, interaction *discordgo.InteractionCreate, command *discordgo.ApplicationCommand, errCommand error) error {
	guild, err := session.Guild(interaction.GuildID)
	if err != nil {
		return fmt.Errorf("getting guild: %w", err)
	}

	if _, err := session.ChannelMessageSendEmbed(supportErrorLogChannel, embeds.ErrorLogEmbed(command, guild,
		interaction.ApplicationCommandData().Options, errCommand)); err != nil {
		return fmt.Errorf("sending message to support channel: %w", err)
	}

	return nil
}

// this function is called when instantiating the music cog
func (m *PlayerCog) RegisterCommands(session *discordgo.Session) {
	commandMapping := slices.Collect(maps.Values(m.getApplicationCommands()))
	commandsToRegister := funcs.Map(commandMapping, func(ac *commands.ApplicationCommand) *discordgo.ApplicationCommand {
		return ac.CommandConfiguration
	})

	for _, command := range commandsToRegister {
		if _, err := session.ApplicationCommandCreate(session.State.Application.ID, "", command); err != nil {
			panic(fmt.Errorf("creating command %s: %w", command.Name, err))
		}
	}

	// This handler will delegate all commands to their respective handler.
	session.AddHandler(m.commandHandler)
	// Handler for when members join or leave a voice channel.
	session.AddHandler(m.voiceStateUpdateEvent)
	// Handler for when bot is kicked out of a guild.
	session.AddHandler(m.guildDeleteEvent)
}

func (m *PlayerCog) getApplicationCommands() map[string]*commands.ApplicationCommand {
	return map[string]*commands.ApplicationCommand{
		"play": {
			Handler: m.play,
			CommandConfiguration: &discordgo.ApplicationCommand{
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
		"play-likes": {
			Handler: m.playLikes,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "play-likes",
				Description: "Plays songs from a member's liked tracks",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "member",
						Description: "A member from your server",
						Type:        discordgo.ApplicationCommandOptionUser,
						Required:    true,
					},
				},
			},
		},
		"pause": {
			Handler: m.pause,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "pause",
				Description: "Pauses the current track playing",
			},
		},
		"resume": {
			Handler: m.resume,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "resume",
				Description: "Resume the current track",
			},
		},
		"skip": {
			Handler: m.skip,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "skip",
				Description: "Skips the current track playing",
			},
		},
		"rewind": {
			Handler: m.rewind,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "rewind",
				Description: "Rewinds to the previous track in the queue",
			},
		},
		"swap": {
			Handler: m.swap,
			CommandConfiguration: &discordgo.ApplicationCommand{
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
			Handler: m.shuffle,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "shuffle",
				Description: "Shuffles the music queue",
			},
		},
		"clear": {
			Handler: m.clear,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "clear",
				Description: "Clears the entire music queue",
			},
		},
		"queue": {
			Handler: m.queue,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "queue",
				Description: "Displays the music queue",
			},
		},
		"spice": {
			Handler: m.spice,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "spice",
				Description: "Add recommended songs to the queue based on the current song playing",
			},
		},
		"playerview": {
			Handler: m.playerview,
			CommandConfiguration: &discordgo.ApplicationCommand{
				Name:        "playerview",
				Description: "Displays the current music player interface",
			},
		},
	}
}

func (m *PlayerCog) retrieveTracks(ctx context.Context, audioType audiotype.SupportedAudioType, query string, trackRetriever TrackDataRetriever) (*audiotype.Data, error) {
	trackData, err := trackRetriever.GetTracksData(ctx, audioType, query)
	if err != nil {
		return nil, fmt.Errorf("retrieving tracks: %w", err)
	}

	return trackData, nil
}
