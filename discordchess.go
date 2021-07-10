package discordchess

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/notnil/chess"
	chessimage "github.com/notnil/chess/image"
	"github.com/notnil/chess/uci"
)

type GameError string

func (e GameError) Error() string { return string(e) }

var ErrNoGame = GameError("No game in progress")

var help = "" +
	"Help:\n" +
	"  `%[1]shelp` - show this\n" +
	"  `%[1]splay @player1 @player2` - starts a game\n" +
	"  `%[1]smove <move>` - do a move in algebraic notation\n" +
	"  `%[1]sboard` - shows the board\n" +
	"  `%[1]sresign` - resigns the game\n"

type ChessHandler struct {
	prefix    string
	channelRE *regexp.Regexp
	states    *state
}

func New(cmdPrefix, channelRe string) *ChessHandler {
	re := regexp.MustCompile(channelRe)

	return &ChessHandler{
		prefix:    cmdPrefix,
		channelRE: re,
		states: &state{
			games: make(map[string]*game),
		},
	}
}

func (c *ChessHandler) MessageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	err := c.messageCreateHandler(s, m)
	if e, ok := err.(GameError); ok {
		s.MessageReactionAdd(m.ChannelID, m.ID, `❌`)
		if e != "" {
			s.ChannelMessageSendReply(
				m.ChannelID,
				string(e),
				&discordgo.MessageReference{
					ChannelID: m.ChannelID,
					MessageID: m.ID,
				})
			// s.ChannelMessageSend(m.ChannelID, string(e))
		}
		return
	}
	if err != nil {
		log.Println("unhandled error:", err)
	}
}

func (c *ChessHandler) messageCreateHandler(s *discordgo.Session, m *discordgo.MessageCreate) error {
	if !strings.HasPrefix(m.Content, c.prefix) {
		return nil
	}

	// TODO check if channel starts with chess- or something

	cmd := strings.Fields(
		strings.Replace(m.Content, c.prefix, "", 1),
	)

	if len(cmd) == 0 {
		return nil
	}

	switch cmd[0] {
	case "say":
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			m.Content[len(c.prefix)+4:],
		)
		return err
	case "help":
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf(help, c.prefix),
		)
		return err
	case "play":
		if g := c.states.game(m.ChannelID); g != nil {
			return GameError(fmt.Sprintf("Game in Process <@%s> vs <@%s>", g.whiteID, g.blackID))
		}

		// check for mentions
		if len(cmd) != 3 || len(m.Mentions) != 2 {
			return GameError(fmt.Sprintf("Start a game with `%splay @player1 @player2`", c.prefix))
		}

		// verify channel name
		channel, err := s.Channel(m.ChannelID)
		if err != nil {
			return err
		}
		if !c.channelRE.MatchString(channel.Name) {
			return GameError("wrong room")
		}

		if err := s.MessageReactionAdd(m.ChannelID, m.ID, "✅"); err != nil {
			return err
		}

		// if one of the mentions is the bot we initialize internal stockfish in this game
		initUCI := false
		if m.Mentions[0].ID == s.State.User.ID || m.Mentions[1].ID == s.State.User.ID {
			if _, err := s.ChannelMessageSend(m.ChannelID, "Trying to play with AI"); err != nil {
				return err
			}
			initUCI = true
		}

		g, err := c.states.newGame(
			m.ChannelID,
			m.Mentions[0].ID,
			m.Mentions[1].ID,
			initUCI,
		)
		if err != nil {
			return GameError(fmt.Sprint("Error starting game: ", err))
		}

		return c.checkOutcome(g, s, m.ChannelID)

	case "move":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}

		if m.Author.ID != g.turn() {
			return GameError("")
		}

		if len(cmd) < 2 {
			_, err := s.ChannelMessageSendReply(
				m.ChannelID,
				fmt.Sprint("Valid moves:", validMovesStr(g)),
				&discordgo.MessageReference{
					ChannelID: m.ChannelID,
					MessageID: m.ID,
				},
			)
			return err
		}

		if err := g.MoveStr(cmd[1]); err != nil {
			return GameError(fmt.Sprint("Invalid move\nAvailable: ", validMovesStr(g)))
		}

		if err := s.MessageReactionAdd(m.ChannelID, m.ID, "✅"); err != nil {
			return err
		}

		return c.checkOutcome(g, s, m.ChannelID)

	case "board":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}

		return c.checkOutcome(g, s, m.ChannelID)
	// Useless for now but might be usefull if we have some kind of ranking
	case "draw":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}

		if m.Author.ID != g.whiteID && m.Author.ID != g.blackID {
			return GameError("")
		}
		if err := s.MessageReactionAdd(m.ChannelID, m.ID, "✅"); err != nil {
			return err
		}

		if g.draw(m.Author.ID) {
			g.Draw(chess.DrawOffer)
			return c.checkOutcome(g, s, m.ChannelID)
		}

		other := g.whiteID
		if other == m.Author.ID {
			other = g.blackID
		}
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf("<@%s> send `!draw` to accept", other),
		)
		return err

	case "resign":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}

		if m.Author.ID != g.turn() {
			return GameError("")
		}

		g.Resign(g.Position().Turn())

		return c.checkOutcome(g, s, m.ChannelID)
	}
	return nil
}

// checkOutcome will send the board, check for outcome and send the game status
// if game is over it will delete from game states
// if the turn() id is same as bot it will use uci to make a move and recheck
// outcome.
func (c *ChessHandler) checkOutcome(g *game, s *discordgo.Session, channelID string) error {
	if err := sendBoard(g, s, channelID); err != nil {
		log.Println("failed to rasterize the board:", err)
		// Send the board in text mode if sendBoard fails
		_, err := s.ChannelMessageSend(
			channelID,
			fmt.Sprintf("```\n%s\n```\n", g.Position().Board().Draw()),
		)
		if err != nil {
			return err
		}
	}

	if o := g.Outcome(); o != chess.NoOutcome {
		return c.GameOver(g, s, channelID)
	}
	if _, err := s.ChannelMessageSend(channelID, fmt.Sprintf("<@%s> turn!", g.turn())); err != nil {
		return err
	}

	// Bot move area
	if s.State.User.ID != g.turn() {
		return nil
	}
	err := g.eng.Run(
		uci.CmdPosition{Position: g.Position()},
		uci.CmdGo{MoveTime: time.Second / 100},
	)
	if err != nil {
		return err
	}
	if err := g.Move(g.eng.SearchResults().BestMove); err != nil {
		return err
	}
	// yeah check again cause bot moved, unless we are running @bot @bot
	// which is virtually impossible, this should be safe
	return c.checkOutcome(g, s, channelID)
}

// sendBoard executes imagemagick convert command to rasterize svg to a raster
// format.
func sendBoard(g *game, s *discordgo.Session, channelID string) error {
	// if only there was something to rasterize svg
	// or we eventually write an image.Image
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
				{Name: "board.jpg", Reader: rd},
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

func (c *ChessHandler) GameOver(g *game, s *discordgo.Session, channelID string) error {
	defer c.states.done(channelID)

	var winner string
	method := g.Method().String()

	whiteStatus, whiteEmoji := "draw", ""
	blackStatus, blackEmoji := "draw", ""

	switch g.Outcome() {
	case chess.WhiteWon:
		winner = g.whiteID
		whiteStatus, whiteEmoji = "Win", ":tada:"
		blackStatus, blackEmoji = "Lose", ":thumbsdown:"
	case chess.BlackWon:
		winner = g.blackID
		whiteStatus, whiteEmoji = "Lose", ":thumbsdown:"
		blackStatus, blackEmoji = "Win", ":tada:"
	}

	var avatarurl string
	if winner != "" {
		user, err := s.User(winner)
		if err != nil {
			return err
		}
		avatarurl = user.AvatarURL("128x128")
	}

	_, err := s.ChannelMessageSendEmbed(
		channelID,
		&discordgo.MessageEmbed{
			Title:       "Game over",
			Description: method,
			Color:       0x5d<<16 | 0xC9<<8 | 0xE2, // gopher color "#5DC9E2"
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: avatarurl,
			},
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   whiteStatus,
					Value:  fmt.Sprintf("%s <@%s>", whiteEmoji, g.whiteID),
					Inline: true,
				},
				{
					Name:   blackStatus,
					Value:  fmt.Sprintf("%s <@%s>", blackEmoji, g.blackID),
					Inline: true,
				},
				{
					Name:   "Game:",
					Value:  g.String(),
					Inline: false,
				},
			},
		},
	)
	return err
}

func validMovesStr(g *game) string {
	buf := &bytes.Buffer{}
	moves := g.ValidMoves()
	fmt.Fprintf(buf, "\n```")
	enc := chess.AlgebraicNotation{}
	var lastPiece chess.Piece
	for _, m := range moves {
		p := g.Position().Board().Piece(m.S1())
		if p != lastPiece {
			fmt.Fprintf(buf, "\n%s - ", p.String())
			lastPiece = p
		}
		fmt.Fprintf(buf, "%s ",
			enc.Encode(g.Position(), m),
		)
	}
	fmt.Fprintf(buf, "\n```")
	return buf.String()
}
