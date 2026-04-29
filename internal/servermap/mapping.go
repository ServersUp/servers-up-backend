package servermap

import (
	"errors"
	"sort"
)

// Mapping mirrors the JSON shape of server-mapping.json stored in S3.
type Mapping struct {
	Games map[string]Game `json:"games"`
}

type Game struct {
	Provider string            `json:"provider"`
	Servers  map[string]Server `json:"servers"`
}

type Server struct {
	Region     string `json:"region"`
	Identifier any    `json:"identifier"`
}

var (
	ErrMissingGame   = errors.New("missing game")
	ErrMissingServer = errors.New("missing server")
	ErrUnknownGame   = errors.New("unknown game")
	ErrUnknownServer = errors.New("unknown server")
)

func (m Mapping) ListGames() []string {
	if len(m.Games) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.Games))
	for k := range m.Games {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (m Mapping) ListServers(gameID string) ([]string, error) {
	gameID = NormalizeKey(gameID)
	if gameID == "" {
		return nil, ErrMissingGame
	}
	game, ok := m.Games[gameID]
	if !ok {
		return nil, ErrUnknownGame
	}
	out := make([]string, 0, len(game.Servers))
	for k := range game.Servers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Lookup finds a game and server after normalizing inputs.
// It returns normalized keys plus the game and server entries.
func (m Mapping) Lookup(gameInput, serverInput string) (gameID string, game Game, serverName string, server Server, err error) {
	gameID = NormalizeKey(gameInput)
	serverName = NormalizeKey(serverInput)

	if gameID == "" {
		return "", Game{}, "", Server{}, ErrMissingGame
	}
	if serverName == "" {
		return gameID, Game{}, "", Server{}, ErrMissingServer
	}

	g, ok := m.Games[gameID]
	if !ok {
		return gameID, Game{}, serverName, Server{}, ErrUnknownGame
	}

	s, ok := g.Servers[serverName]
	if !ok {
		return gameID, g, serverName, Server{}, ErrUnknownServer
	}

	return gameID, g, serverName, s, nil
}
