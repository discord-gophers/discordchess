package main

import (
	"github.com/notnil/chess"
)

type game struct {
	*chess.Game

	whiteID, blackID string
	curID            string
}

func newGame(whiteID, blackID string) *game {
	return &game{
		whiteID: whiteID,
		blackID: blackID,
		curID:   whiteID,
		Game:    chess.NewGame(),
	}
}
