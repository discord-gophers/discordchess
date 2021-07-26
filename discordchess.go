package discordchess

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/DiscordGophers/discordchess/chessimage"
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
	"  `%[1]sresign` - resigns the game\n" +
	"  `%[1]sdraw` - offer draw\n"

type ChessHandler struct {
	prefix     string
	channelRE  *regexp.Regexp
	adminRoles map[string]struct{}
	drawer     *chessimage.Drawer
	states     *state
}

func New(cmdPrefix, channelRe string, adminRoles []string) (*ChessHandler, error) {
	re := regexp.MustCompile(channelRe)

	roleMap := map[string]struct{}{}
	for _, r := range adminRoles {
		roleMap[r] = struct{}{}
	}

	drawer, err := chessimage.NewDrawer()
	if err != nil {
		return nil, err
	}

	c := &ChessHandler{
		prefix:     cmdPrefix,
		channelRE:  re,
		adminRoles: roleMap,
		drawer:     drawer,
		states: &state{
			games: make(map[string]*game),
		},
	}
	return c, nil
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
	case "cool":
		g := c.states.game(m.ChannelID)
		if g == nil {
			return ErrNoGame
		}
		return c.coolThing(g, s, m.ChannelID)
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
			fmt.Sprintf("<@%s> send `%sdraw` to accept", other, c.prefix),
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
	if err := c.sendBoard(g, s, channelID); err != nil {
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

// GameOver sends game finish Card.
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

	gi, err := c.boardGIF(g)
	if err != nil {
		return err
	}
	_, err = s.ChannelMessageSendEmbed(
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
	if err != nil {
		return err
	}

	gifr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(gif.EncodeAll(pw, gi))
	}()

	_, err = s.ChannelFileSend(channelID, "board.gif", gifr)
	return err
}

func (c *ChessHandler) coolThing(g *game, s *discordgo.Session, channelID string) error {
	gi, err := c.boardGIF(g)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(gif.EncodeAll(pw, gi))
	}()

	_, err = s.ChannelFileSend(channelID, "board.gif", pr)
	return err
}

// Draw using the drawer :tada:
func (c *ChessHandler) sendBoard(g *game, s *discordgo.Session, channelID string) error {
	pr, pw := io.Pipe()
	defer pr.Close()

	im, err := c.boardImage(g)
	if err != nil {
		return err
	}
	go func() {
		pw.CloseWithError(png.Encode(pw, im))
	}()

	if _, err := s.ChannelFileSend(channelID, "board.png", pr); err != nil {
		return err
	}

	if o := book.Find(g.Moves()); o != nil {
		_, err = s.ChannelMessageSend(channelID, o.Title())
	}
	return err
}

func (c *ChessHandler) boardImage(g *game) (*image.RGBA, error) {
	markColor := color.RGBA{55, 55, 155, 100}
	marks := []chessimage.Mark{}

	moves := g.Moves()
	if len(moves) != 0 {
		last := moves[len(moves)-1]
		marks = []chessimage.Mark{
			{
				Color: markColor,
				Pos: [][2]int{
					{int(last.S1().File()), 7 - int(last.S1().Rank())},
					{int(last.S2().File()), 7 - int(last.S2().Rank())},
				},
			},
		}
	}
	return c.drawer.Image(g.Position().String(), marks...)
}

func (c *ChessHandler) boardGIF(g *game) (*gif.GIF, error) {
	gi := gif.GIF{
		Config: image.Config{
			Width:  512,
			Height: 512,
		},
	}

	markColor := color.RGBA{100, 100, 200, 255}
	moves := g.Moves()
	gg := chess.NewGame()
	frame, err := c.drawer.ImagePaletted(gg.Position().String())
	if err != nil {
		return nil, err
	}
	gi.Image = append(gi.Image, frame)
	gi.Delay = append(gi.Delay, 150)

	for i, m := range moves {
		gg.Move(m)
		frame, err := c.drawer.ImagePaletted(
			gg.Position().String(),
			chessimage.Mark{
				Color: markColor,
				Pos: [][2]int{
					{int(m.S1().File()), 7 - int(m.S1().Rank())},
					{int(m.S2().File()), 7 - int(m.S2().Rank())},
				},
			},
		)
		if err != nil {
			return nil, err
		}
		gi.Image = append(gi.Image, frame)
		delay := 150
		if i == len(moves)-1 {
			delay = 500
		}
		gi.Delay = append(gi.Delay, delay)
	}
	return &gi, nil
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
