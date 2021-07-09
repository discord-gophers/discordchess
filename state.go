package main

import "sync"

var gameStates = &state{
	games: make(map[string]*game),
}

type state struct {
	games map[string]*game
	mu    sync.Mutex
}

func (s *state) newGame(channelID, whiteID, blackID string) *game {
	s.mu.Lock()
	defer s.mu.Unlock()

	g := newGame(whiteID, blackID)
	s.games[channelID] = g

	return g
}

func (s *state) game(channelID string) *game {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.games[channelID]
}

func (s *state) done(channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.games, channelID)
}
