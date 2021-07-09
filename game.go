package main

import (
	"github.com/notnil/chess"
)

type game struct {
	*chess.Game

	whiteID, blackID string
}

func newGame(whiteID, blackID string) *game {
	return &game{
		whiteID: whiteID,
		blackID: blackID,
		Game:    chess.NewGame(),
	}
}
