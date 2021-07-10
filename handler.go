package main

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/notnil/chess"
	chessimage "github.com/notnil/chess/image"
)

var help = "" +
	"Help:\n" +
	"  `%[1]shelp` - show this\n" +
	"  `%[1]splay @player1 @player2` - starts a game\n" +
	"  `%[1]smove <move>` - do a move in algebraic notation\n" +
	"  `%[1]sboard` - shows the board\n" +
	"  `%[1]sresign` - resigns the game\n"

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

	// TODO: {lpf} Add Draw offer command?
	switch cmd[0] {
	case "help":
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf(help, prefix),
		)
		return
	case "play":
		if gameStates.game(m.ChannelID) != nil {
			s.ChannelMessageSend(m.ChannelID, "Game in progress")
			return
		}

		// check for mentions
		if len(cmd) != 3 || len(m.Mentions) != 2 {
			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf("Start a game with `%splay @player1 @player2`", prefix),
			)
			return
		}
		s.MessageReactionAdd(m.ChannelID, m.ID, "✅")

		g := gameStates.newGame(
			m.ChannelID,
			m.Mentions[0].ID,
			m.Mentions[1].ID,
		)

		if err := sendBoard(g, s, m.ChannelID); err != nil {
			log.Println("board fail:", err)
		}
		sendOutcome(g, s, m.ChannelID)

	case "move":
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		if m.Author.ID != g.turn() {
			s.MessageReactionAdd(m.ChannelID, m.ID, `❌`)
			return
		}

		if len(cmd) < 2 {
			buf := &bytes.Buffer{}
			moves := g.ValidMoves()
			fmt.Fprintf(buf, "Valid moves:\n```\n")
			enc := chess.AlgebraicNotation{}
			for i, m := range moves {
				if i != 0 {
					fmt.Fprintf(buf, ", ")
				}
				p := g.Position().Board().Piece(m.S1())
				fmt.Fprintf(buf, "%s %s",
					p.String(),
					enc.Encode(g.Position(), m),
				)
			}
			fmt.Fprintf(buf, "\n```")
			s.ChannelMessageSend(m.ChannelID, buf.String())
			return
		}

		if err := g.MoveStr(cmd[1]); err != nil {
			s.MessageReactionAdd(m.ChannelID, m.ID, `❌`)
			// TODO: {lpf} proper error message
			s.ChannelMessageSend(m.ChannelID, fmt.Sprint("Invalid move: ", err))
			return
		}

		s.MessageReactionAdd(m.ChannelID, m.ID, "✅")

		if err := sendBoard(g, s, m.ChannelID); err != nil {
			log.Println("board fail:", err)
		}

		sendOutcome(g, s, m.ChannelID)

	case "board":
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		if err := sendBoard(g, s, m.ChannelID); err != nil {
			log.Println("board fail:", err)
		}
		sendOutcome(g, s, m.ChannelID)

	case "resign":
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}

		if m.Author.ID != g.turn() {
			s.MessageReactionAdd(m.ChannelID, m.ID, `❌`)
			return
		}

		g.Resign(g.Position().Turn())
		sendOutcome(g, s, m.ChannelID)

		gameStates.done(m.ChannelID)

	default:
		g := gameStates.game(m.ChannelID)
		if g == nil {
			s.ChannelMessageSend(m.ChannelID, "No game in progress")
			return
		}
	}
}

func sendBoard(g *game, s *discordgo.Session, channelID string) error {
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
		moves := g.Moves()
		if len(moves) == 0 {
			chessimage.SVG(wr, g.Position().Board())
			return
		}
		last := moves[len(moves)-1]
		yellow := color.RGBA{255, 255, 0, 1}
		chessimage.SVG(
			wr,
			g.Position().Board(),
			chessimage.MarkSquares(yellow, last.S1(), last.S2()),
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
				{Name: "board.png", Reader: rd},
			},
		},
	)
	if err != nil {
		return err
	}

	if o := book.Find(g.Moves()); o != nil {
		_, err = s.ChannelMessageSend(channelID, o.Title())
	}
	return err
}

// TODO: {lpf} sendOutcome could be renamed to checkOutcome or so, since it
// does more than just sending message, like changing gameStates to remove the game
func sendOutcome(g *game, s *discordgo.Session, channelID string) {
	switch g.Outcome() {
	case chess.WhiteWon:
		// TODO: {lpf} could be a nice message with a bigger PFP and all that
		s.ChannelMessageSend(channelID, fmt.Sprintf(":tada: <@%s> Won %s!!\n%s", g.whiteID, g.Method(), g.String()))
		gameStates.done(channelID)
	case chess.BlackWon:
		s.ChannelMessageSend(channelID, fmt.Sprintf(":tada: <@%s> Won %s!!\n%s", g.blackID, g.Method(), g.String()))
		gameStates.done(channelID)
	case chess.Draw:
		s.ChannelMessageSend(channelID, fmt.Sprintf("Draw %s!!\n%v", g.Method(), g.String()))
		gameStates.done(channelID)
	case chess.NoOutcome:
		s.ChannelMessageSend(channelID, fmt.Sprintf("<@%s> turn!", g.turn()))
	}
}
