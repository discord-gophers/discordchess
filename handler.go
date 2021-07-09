package main

import (
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate) {

	prefix := os.Getenv("CMD_PREFIX")

	if !strings.HasPrefix(m.Content, os.Getenv("CMD_PREFIX")) {
		return
	}

	// TODO check if channel starts with chess- or something

	cmd := strings.Fields(
		strings.Replace(m.Content, prefix, "", 1),
	)

	if len(cmd) == 0 {
		return
	}

	switch cmd[0] {
	case "play":
		if gameStates.game(m.ChannelID) != nil {
			s.ChannelMessageSend(m.ChannelID, "Game in progress")
			return
		}

		// check for mentions
		if len(cmd) != 3 || len(m.Mentions) != 2 {
			s.ChannelMessageSend(
				m.ChannelID,
				"Start a game with `"+prefix+"play @player1 @player2`",
			)
			return
		}

		g := gameStates.newGame(
			m.ChannelID,
			m.Mentions[0].ID,
			m.Mentions[1].ID,
		)
		_ = g

		/*
			buf := new(bytes.Buffer)
			svg.SVG(
				buf,
				g.Position().Board(),
			)

			s.ChannelMessageSendComplex(
				m.ChannelID,
				&discordgo.MessageSend{
					Content: "White to move",
					Files: []*discordgo.File{
						&discordgo.File{
							Name:   "board.png",
							Reader: buf,
						},
					},
				},
			)
		*/

	case "resign":
		if gameStates.game(m.ChannelID) == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		gameStates.resign(m.ChannelID)
		// TODO indicate winner

	default:
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}
	}
}
