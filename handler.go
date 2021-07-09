package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/notnil/chess"
	chessimage "github.com/notnil/chess/image"
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

		if err := sendBoard(s, m.ChannelID, g.Position().Board()); err != nil {
			log.Println("board fail:", err)
		}
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf("<@%s> turn!", g.curID),
		)

	case "move":
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		if m.Author.ID != g.curID {
			s.ChannelMessageSend(m.ChannelID, "nop")
			s.MessageReactionAdd(m.ChannelID, m.Message.ID, ":x:")
			return
		}
		if len(cmd) < 2 {
			s.ChannelMessageSend(m.ChannelID, "Valid moves: "+fmt.Sprint(g.ValidMoves()))
			return
		}
		if err := g.MoveStr(cmd[1]); err != nil {
			// TODO: {lpf} proper error message
			s.ChannelMessageSend(m.ChannelID, "Invalid move: "+fmt.Sprint(err))
			return
		}
		s.MessageReactionAdd(m.ChannelID, m.Message.ID, ":white_check_mark:")

		if err := sendBoard(s, m.ChannelID, g.Position().Board()); err != nil {
			log.Println("board fail:", err)
		}

		// TODO: {lpf} a method on game state might be better?
		curID := g.whiteID
		if g.curID == g.whiteID {
			curID = g.blackID
		}
		g.curID = curID

		switch g.Outcome() {
		case chess.WhiteWon:
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Won !!\n%v", g.whiteID, g.Moves()))
			gameStates.done(m.ChannelID)
		case chess.BlackWon:
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Won !!\n%v", g.blackID, g.Moves()))
			gameStates.done(m.ChannelID)
		case chess.Draw:
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Draw !!\n%v", g.Moves()))
			gameStates.done(m.ChannelID)
		case chess.NoOutcome:
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> turn!", g.curID))
		}

	case "resign":
		if gameStates.game(m.ChannelID) == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		gameStates.done(m.ChannelID)
		// TODO indicate winner

	default:
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}
	}
}

func sendBoard(s *discordgo.Session, channelID string, board *chess.Board) error {
	cmd := exec.Command("convert", "svg:-", "png:-")
	wr, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	rd, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	go func() {
		defer wr.Close()
		chessimage.SVG(
			wr,
			board,
		)
	}()

	go func() {
		defer rd.Close()
		cmd.Run()
	}()

	_, err = s.ChannelMessageSendComplex(
		channelID,
		&discordgo.MessageSend{
			Files: []*discordgo.File{
				{
					Name:   "board.png",
					Reader: rd,
				},
			},
		},
	)
	return err
}
