package discordchess

import (
	"sync"

	"github.com/notnil/chess/opening"
)

// global board drawer
var drawer = func() *Drawer {
	d, err := NewDrawer()
	if err != nil {
		panic(err)
	}
	return d
}()
var book *opening.BookECO = opening.NewBookECO()

// this could be directly in the ChessHandler struct
type state struct {
	games map[string]*game
	mu    sync.Mutex
}

func (s *state) newGame(channelID, whiteID, blackID string, initUCI bool) (*game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	g, err := newGame(whiteID, blackID, initUCI)
	if err != nil {
		return nil, err
	}
	s.games[channelID] = g

	return g, nil
}

func (s *state) game(channelID string) *game {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.games[channelID]
}

func (s *state) done(channelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if g := s.games[channelID]; g != nil {
		g.Close()
	}

	delete(s.games, channelID)
}
