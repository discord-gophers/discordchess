package discordchess

import (
	"time"

	"github.com/notnil/chess"
	"github.com/notnil/chess/uci"
)

type game struct {
	*chess.Game

	whiteID, blackID string
	// TODO: {lpf} this can be used later to bust the game if stuck
	createdAt  time.Time
	lastMoveAt time.Time

	drawWhite bool
	drawBlack bool
	// optional uci engine
	eng *uci.Engine
}

func newGame(whiteID, blackID string, initUCI bool) (*game, error) {
	var eng *uci.Engine
	if initUCI {
		e, err := uci.New("stockfish")
		if err != nil {
			return nil, err
		}
		if err := e.Run(uci.CmdUCI, uci.CmdIsReady, uci.CmdUCINewGame); err != nil {
			return nil, err
		}
		eng = e
	}

	g := &game{
		whiteID: whiteID,
		blackID: blackID,

		eng:        eng,
		createdAt:  time.Now().UTC(),
		lastMoveAt: time.Now().UTC(),

		Game: chess.NewGame(chess.UseNotation(chess.AlgebraicNotation{})),
	}
	return g, nil
}

func (g *game) Close() {
	if g.eng != nil {
		g.eng.Close()
	}
}

func (g *game) MoveStr(m string) error {
	g.drawWhite = false
	g.drawBlack = false

	g.lastMoveAt = time.Now().UTC()
	return g.Game.MoveStr(m)
}

func (g *game) draw(id string) bool {
	switch id {
	case g.whiteID:
		g.drawWhite = true
	case g.blackID:
		g.drawBlack = true
	}
	return g.drawWhite && g.drawBlack
}

func (g *game) turn() string {
	return g.player(g.Position().Turn())
}

func (g *game) player(c chess.Color) string {
	switch c {
	case chess.White:
		return g.whiteID
	case chess.Black:
		return g.blackID
	default:
		return ""
	}
}
