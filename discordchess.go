package discordchess

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/notnil/chess"
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
	prefix     string
	channelRE  *regexp.Regexp
	adminRoles map[string]struct{}
	states     *state
}

func New(cmdPrefix, channelRe string, adminRoles []string) *ChessHandler {
	re := regexp.MustCompile(channelRe)

	roleMap := map[string]struct{}{}
	for _, r := range adminRoles {
		roleMap[r] = struct{}{}
	}

	return &ChessHandler{
		prefix:     cmdPrefix,
		channelRE:  re,
		adminRoles: roleMap,
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

	cmd := strings.Fields(
		strings.Replace(m.Content, c.prefix, "", 1),
	)

	if len(cmd) == 0 {
		return nil
	}

	switch cmd[0] {
	case "cancel":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}
		allow := false
		// TODO: {lpf} I'm not sure if we need to include guildIDs or
		// just role to avoid other guild admins to cancel the game in
		// 'this' guild
		for _, mr := range m.Member.Roles {
			r := fmt.Sprintf("%s:%s", m.GuildID, mr)
			if _, ok := c.adminRoles[r]; ok {
				allow = true
				break
			}
		}
		if !allow {
			return GameError("")
		}
		if err := g.Draw(chess.DrawOffer); err != nil {
			return err
		}
		return c.checkOutcome(g, s, m.ChannelID)
	case "say":
		msg := strings.Replace(m.Content, c.prefix+"say", "", 1)
		if msg == "" {
			return nil
		}
		_, err := s.ChannelMessageSend(
			m.ChannelID,
			msg,
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
		uci.CmdGo{MoveTime: time.Second / 10},
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

// Draw using the drawer :tada:
func sendBoard(g *game, s *discordgo.Session, channelID string) error {
	markColor := color.RGBA{55, 55, 155, 100}
	marks := []DrawerMark{}

	im := image.NewRGBA(image.Rect(0, 0, 512, 512))
	moves := g.Moves()
	if len(moves) != 0 {
		last := moves[len(moves)-1]
		marks = []DrawerMark{
			{markColor, int(last.S1().File()), 7 - int(last.S1().Rank())},
			{markColor, int(last.S2().File()), 7 - int(last.S2().Rank())},
		}
	}
	if err := drawer.Draw(im, g.Position().String(), marks...); err != nil {
		return err
	}

	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		pw.CloseWithError(png.Encode(pw, im))
	}()

	_, err := s.ChannelMessageSendComplex(
		channelID,
		&discordgo.MessageSend{
			Files: []*discordgo.File{
				{Name: "board.jpg", Reader: pr},
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
