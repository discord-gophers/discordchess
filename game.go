package main

import (
	"time"

	"github.com/notnil/chess"
)

type game struct {
	*chess.Game

	whiteID, blackID string
	// TODO: {lpf} this can be used later to bust the game if stuck
	createdAt  time.Time
	lastMoveAt time.Time
}

func newGame(whiteID, blackID string) *game {
	return &game{
		whiteID: whiteID,
		blackID: blackID,

		createdAt:  time.Now().UTC(),
		lastMoveAt: time.Now().UTC(),

		Game: chess.NewGame(chess.UseNotation(chess.AlgebraicNotation{})),
	}
}

func (g *game) MoveStr(m string) error {
	g.lastMoveAt = time.Now().UTC()
	return g.Game.MoveStr(m)
}

func (g *game) turn() string {
	if g.Position().Turn() == chess.White {
		return g.whiteID
	}
	return g.blackID
}
