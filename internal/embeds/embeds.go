package embeds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TeddyKahwaji/spice-tunes-go/pkg/audiotype"
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/funcs"
	"github.com/bwmarrin/discordgo"
)

const (
	LightPink int = 0xd5a7b4
	Brown     int = 0x992D22
	Blurple   int = 0x5865F2
	Purple    int = 0xd333ff
	DarkRed   int = 0x992D22
	DarkGreen int = 0x1F8B4C
	PitchDark int = 0x020B03
)

type Gif string

func (g Gif) String() string {
	return string(g)
}

const (
	noddingHead   Gif = "https://media.giphy.com/media/v1.Y2lkPTc5MGI3NjExZnphaG5wOG5ldjNwaG5qcmt5M3VubWwzY2RkOXVkeWx5cDNha2Y5YyZlcD12MV9naWZzX3NlYXJjaCZjdD1n/Qbm1Oget7e3vVl9uPB/giphy.gif"
	shakingHeadNo Gif = "https://media.giphy.com/media/S5tkhUBHTTWh865paS/giphy.gif"
	notFound      Gif = "https://media.giphy.com/media/piL4e4WusrA4S0KODK/giphy.gif"
	daftPunk      Gif = "https://media.giphy.com/media/blFQljCuW6s9h43SZu/giphy.gif"
	newPlaylist   Gif = "https://media.giphy.com/media/mbrFPmLoNpBh9BrkNS/giphy.gif"
)

func ErrorMessageEmbed(msg string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "❌ **Invalid usage**",
		Description: msg,
		Color:       Brown,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: shakingHeadNo.String(),
		},
	}
}

func GuildAuditEmbed(guild *discordgo.Guild, joined bool) *discordgo.MessageEmbed {
	title := guild.Name + "  has kicked me from their discord 😥"
	color := PitchDark
	if joined {
		title = guild.Name + " has added me to their server! 🤗"
		color = DarkGreen
	}

	return &discordgo.MessageEmbed{
		Title: title,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  "Guild ID: ",
				Value: fmt.Sprintf("`%s`", guild.ID),
			},
		},
		Color: color,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: guild.IconURL(""),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func NotFoundEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Search Query Has No Results",
		Description: "Sorry, I couldn't find any results for your search.\n\nPlease provide a direct `YouTube` or `Spotify` link.",
		Color:       LightPink,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: notFound.String(),
		},
	}
}

func NoLikedTracksFound(member *discordgo.User) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       member.Username + " Has No Liked Tracks",
		Description: "This member hasn't liked any tracks in this server.",
		Color:       LightPink,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: notFound.String(),
		},
	}
}

func LikedSongEmbed(track *audiotype.TrackData) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "👍 Like Recorded",
		Color: Blurple,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: track.TrackImageURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Value: fmt.Sprintf("I've recorded that you liked `%s`", track.TrackName),
			},
		},
	}
}

func SpiceEmbed(tracksAdded int, positionAdded int, addedBy *discordgo.Member) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "**Spice Activated**",
		Description: "`Added recommended tracks to the queue`",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: daftPunk.String(),
		},
		Color: LightPink,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "**Position in queue**",
				Value:  fmt.Sprintf("`%d`", positionAdded),
				Inline: true,
			},
			{
				Name:   "**Enqueued**",
				Value:  fmt.Sprintf("`%d songs`", tracksAdded),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text:    "Added by: " + addedBy.User.Username,
			IconURL: addedBy.User.AvatarURL(""),
		},
	}
}

func TracksSwappedEmbed(member *discordgo.Member, firstTrack *audiotype.TrackData, firstPositionSwapped int, secondTrack *audiotype.TrackData, secondPositionSwapped int) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Description: "✅ **Tracks Swapped**",
		Color:       Blurple,
		Fields: []*discordgo.MessageEmbedField{
			{
				Value: fmt.Sprintf("`%s` has been moved to position: `%d`", firstTrack.TrackName, firstPositionSwapped),
			},
			{
				Value: fmt.Sprintf("`%s` has been moved to position `%d`", secondTrack.TrackName, secondPositionSwapped),
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: noddingHead.String(),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text:    "Swapped by: " + member.User.Username,
			IconURL: member.AvatarURL(""),
		},
	}
}

func QueueEmbed(tracks []*audiotype.TrackData, pageNumber int, totalPages int, separator int, guild *discordgo.Guild) *discordgo.MessageEmbed {
	result := &discordgo.MessageEmbed{
		Title: guild.Name + "'s Queue",
		Color: Blurple,
		Footer: &discordgo.MessageEmbedFooter{
			Text:    fmt.Sprintf("Page %d / %d", pageNumber, totalPages),
			IconURL: guild.IconURL(""),
		},
	}

	queueIndex := ((pageNumber * separator) + 1) - separator
	for _, trackData := range tracks {
		result.Fields = append(result.Fields, &discordgo.MessageEmbedField{
			Value: fmt.Sprintf("```%d: %s```", queueIndex, trackData.TrackName),
		})

		queueIndex++
	}

	return result
}

func MusicPlayerEmbed(trackData *audiotype.TrackData) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Now Playing 🎵",
		Description: trackData.TrackName,
		Color:       LightPink,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "`Length:`",
				Value:  audiotype.FormatDuration(trackData.Duration),
				Inline: true,
			},
			{
				Name:   "`Requested by:`",
				Value:  trackData.Requester,
				Inline: true,
			},
		},
	}
}

func CreatedUserPlaylistEmbed(playlistName string) *discordgo.MessageEmbed {
	const playlistAddCommandID string = "1297318617305841835"
	const playlistPlayCommandID string = "1297318620401238077"

	return &discordgo.MessageEmbed{
		Title: "Playlist Created",
		Color: LightPink,
		Fields: []*discordgo.MessageEmbedField{
			{
				Value: fmt.Sprintf("Playlist `%s` has been created\n\n", playlistName),
			},
			{
				Value: fmt.Sprintf("`-` To add tracks to your playlist use </playlist-add:%s>\n`-` To play your playlist use </playlist-play:%s>", playlistAddCommandID, playlistPlayCommandID),
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: newPlaylist.String(),
		},
	}
}

func DeletedUserPlaylistEmbed(playlistName string) *discordgo.MessageEmbed {
	const playlistCreateCommandID string = "1297318614810099853"
	const playlistPlayCommandID string = "1297318620401238077"

	return &discordgo.MessageEmbed{
		Title: "Playlist Deleted",
		Color: LightPink,
		Fields: []*discordgo.MessageEmbedField{
			{
				Value: fmt.Sprintf("Playlist `%s` has been created\n\n", playlistName),
			},
			{
				Value: fmt.Sprintf("`-` To create a playlist use </playlist-create:%s>\n`-` To play your playlist use </playlist-play:%s>", playlistCreateCommandID, playlistPlayCommandID),
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: noddingHead.String(),
		},
	}
}

func AddedTracksToUserPlaylistEmbed(tracksAdded int, totalTrackCount int, playlistName string, member *discordgo.Member) *discordgo.MessageEmbed {
	const playlistPlayCommandID string = "1297318620401238077"

	return &discordgo.MessageEmbed{
		Color:       LightPink,
		Title:       "🎶 Tracks Added! 🎶",
		Description: fmt.Sprintf("You successfully added **%d** tracks to the playlist **`%s`**!", tracksAdded, playlistName),
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: noddingHead.String(),
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Value: fmt.Sprintf("`-` To play this playlist use </playlist-play:%s>\n", playlistPlayCommandID),
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text:    fmt.Sprintf("Total Tracks: %d", totalTrackCount),
			IconURL: member.User.AvatarURL(""),
		},
	}
}

// This function will return the added songs message embed to the user
// if the added data was a playlist & the playlist metadata field is nil it will
// return an error.
func AddedTracksToQueueEmbed(trackData *audiotype.Data, member *discordgo.Member, position int) (*discordgo.MessageEmbed, error) {
	baseMessageEmbed := discordgo.MessageEmbed{
		Color: LightPink,
		Footer: &discordgo.MessageEmbedFooter{
			Text:    "Added by " + member.User.Username,
			IconURL: member.AvatarURL(""),
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "**Position in queue**",
				Value:  fmt.Sprintf("`%d`", position),
				Inline: true,
			},
		},
	}

	if len(trackData.Tracks) > 1 {
		if trackData.PlaylistData == nil {
			return nil, errors.New("error no playlist data")
		}

		baseMessageEmbed.Description = fmt.Sprintf("**%s** added to queue", trackData.PlaylistData.PlaylistName)

		baseMessageEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: trackData.PlaylistData.PlaylistImageURL,
		}

		baseMessageEmbed.Fields = append(baseMessageEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "**Enqueued**",
			Value:  fmt.Sprintf("`%d` songs", len(trackData.Tracks)),
			Inline: true,
		})
	} else {
		addedTrack := trackData.Tracks[0]

		baseMessageEmbed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: addedTrack.TrackImageURL,
		}

		baseMessageEmbed.Description = fmt.Sprintf("**%s** added to queue", addedTrack.TrackName)

		baseMessageEmbed.Fields = append(baseMessageEmbed.Fields, &discordgo.MessageEmbedField{
			Name:   "**Duration**",
			Value:  fmt.Sprintf("`%s`", audiotype.FormatDuration(addedTrack.Duration)),
			Inline: true,
		})
	}

	return &baseMessageEmbed, nil
}

func MusicPlayerActionEmbed(content string, member discordgo.Member) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Description: content,
		Color:       Blurple,
		Footer: &discordgo.MessageEmbedFooter{
			IconURL: member.AvatarURL(""),
			Text:    "Action initiated by " + member.User.Username,
		},
	}
}

func UnexpectedErrorEmbed() *discordgo.MessageEmbed {
	const supportServerInvite = "https://discord.gg/WsKwCTpKhH"

	return &discordgo.MessageEmbed{
		Title:       "Sorry I could not process your request 🤖 🔥",
		Description: fmt.Sprintf("`-` Sorry an unexpected error occurred\n\n`-` If this continues to happen please join the [support channel](%s)", supportServerInvite),
		Color:       Purple,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: noddingHead.String(),
		},
	}
}

func ErrorLogEmbed(command *discordgo.ApplicationCommand, guild *discordgo.Guild, options []*discordgo.ApplicationCommandInteractionDataOption, err error) *discordgo.MessageEmbed {
	errorLogEmbed := &discordgo.MessageEmbed{
		Title:     fmt.Sprintf("Command %s failed", command.Name),
		Color:     DarkRed,
		Timestamp: time.Now().Format(time.RFC3339),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Error: ",
				Value:  fmt.Sprintf("`%s`", err.Error()),
				Inline: true,
			},
			{
				Name:   "Guild ID: ",
				Value:  fmt.Sprintf("`%s`", guild.ID),
				Inline: true,
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: guild.IconURL(""),
		},
	}

	if len(options) > 0 {
		commandInputs := funcs.Map(options, func(options *discordgo.ApplicationCommandInteractionDataOption) string {
			return fmt.Sprintf("%s: %v", options.Name, options.Value)
		})

		formattedCommandInputs := strings.Join(commandInputs, ",")
		errorLogEmbed.Fields = append(errorLogEmbed.Fields, &discordgo.MessageEmbedField{
			Name:  "Command Inputs",
			Value: fmt.Sprintf("`%s`", formattedCommandInputs),
		})
	}

	return errorLogEmbed
}

func HelpMenuEmbed(commands []*discordgo.ApplicationCommand) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:       "**🤖 Spice Tunes Help Page 💽**",
		Description: "Spice Tunes only supports `/` commands, to view available commands use `/` followed by the desired command",
		Color:       LightPink,
	}

	for _, command := range commands {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("**%s**", command.Name),
			Value:  fmt.Sprintf("``%s``", command.Description),
			Inline: true,
		})
	}

	return embed
}
