package embeds

import (
	"errors"
	"fmt"

	"tunes/pkg/audiotype"

	"github.com/bwmarrin/discordgo"
)

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

func MusicPlayerEmbed(trackData audiotype.TrackData) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "Now Playing 🎵",
		Description: trackData.TrackName,
		Color:       0xd5a7b4,
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

// This function will return the added songs message embed to the user
// if the added data was a playlist & the playlist metadata field is nil it will
// return an error
func AddedTracksEmbed(trackData *audiotype.Data, member *discordgo.Member, position int) (*discordgo.MessageEmbed, error) {
	baseMessageEmbed := discordgo.MessageEmbed{
		Color: 0xd5a7b4,
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
		Color:       0xd5a7b4,
		Footer: &discordgo.MessageEmbedFooter{
			IconURL: member.AvatarURL(""),
			Text:    "Action initiated by " + member.User.Username,
		},
	}
}

func UnexpectedErrorEmbed() *discordgo.MessageEmbed {
	const supportServerInvite = "https://discord.gg/WsKwCTpKhH"
	const botGif = "https://media.giphy.com/media/v1.Y2lkPTc5MGI3NjExZnphaG5wOG5ldjNwaG5qcmt5M3VubWwzY2RkOXVkeWx5cDNha2Y5YyZlcD12MV9naWZzX3NlYXJjaCZjdD1n/Qbm1Oget7e3vVl9uPB/giphy.gif"

	return &discordgo.MessageEmbed{
		Title:       "Sorry I could not process your request 🤖 🔥",
		Description: fmt.Sprintf("`-` Sorry an unexpected error occurred\n\n`-` If this continues to happen please join the [support channel](%s)", supportServerInvite),
		Color:       0xd333ff,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: botGif,
		},
	}
}
