package servermap

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Mapping mirrors the JSON shape of server-mapping.json stored in S3.
type Mapping struct {
	Games map[string]Game `json:"games"`
}

type Game struct {
	Provider string             `json:"provider"`
	Regions  map[string]Region  `json:"regions"`
}

type Region struct {
	Servers map[string]Server `json:"servers"`
}

type Server struct {
	Identifier any `json:"identifier"`
}

var (
	ErrMissingGame   = errors.New("missing game")
	ErrMissingRegion = errors.New("missing region")
	ErrMissingServer = errors.New("missing server")
	ErrUnknownGame   = errors.New("unknown game")
	ErrUnknownRegion = errors.New("unknown region")
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

func (m Mapping) ListRegions(gameID string) ([]string, error) {
	gameID = NormalizeKey(gameID)
	if gameID == "" {
		return nil, ErrMissingGame
	}
	game, ok := m.Games[gameID]
	if !ok {
		return nil, ErrUnknownGame
	}
	out := make([]string, 0, len(game.Regions))
	for k := range game.Regions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func (m Mapping) ListServers(gameID, regionID string) ([]string, error) {
	gameID = NormalizeKey(gameID)
	regionID = NormalizeKey(regionID)
	if gameID == "" {
		return nil, ErrMissingGame
	}
	if regionID == "" {
		return nil, ErrMissingRegion
	}
	game, ok := m.Games[gameID]
	if !ok {
		return nil, ErrUnknownGame
	}
	region, ok := game.Regions[regionID]
	if !ok {
		return nil, ErrUnknownRegion
	}
	out := make([]string, 0, len(region.Servers))
	for k := range region.Servers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// Lookup finds a game, region, and server after normalizing inputs.
// It returns normalized keys plus the game and server entries.
func (m Mapping) Lookup(gameInput, regionInput, serverInput string) (gameID, regionKey, serverKey string, game Game, server Server, err error) {
	gameID = NormalizeKey(gameInput)
	regionKey = NormalizeKey(regionInput)
	serverKey = NormalizeKey(serverInput)

	if gameID == "" {
		return "", "", "", Game{}, Server{}, ErrMissingGame
	}
	if regionKey == "" {
		return gameID, "", "", Game{}, Server{}, ErrMissingRegion
	}
	if serverKey == "" {
		return gameID, regionKey, "", Game{}, Server{}, ErrMissingServer
	}

	g, ok := m.Games[gameID]
	if !ok {
		return gameID, regionKey, serverKey, Game{}, Server{}, ErrUnknownGame
	}

	r, ok := g.Regions[regionKey]
	if !ok {
		return gameID, regionKey, serverKey, g, Server{}, ErrUnknownRegion
	}

	s, ok := r.Servers[serverKey]
	if !ok {
		return gameID, regionKey, serverKey, g, Server{}, ErrUnknownServer
	}

	return gameID, regionKey, serverKey, g, s, nil
}

// DisplayLabel returns the display label for a known game/region/server tuple (e.g. "wow-eu-kazzak").
func DisplayLabel(gameID, regionKey, serverKey string) string {
	return fmt.Sprintf("%s-%s-%s", gameID, regionKey, serverKey)
}

// HumanLabel maps a technical server ID (provider#region#identifier) to a display label (game-region-server).
// Returns technicalServerID unchanged when the ID is malformed or not found in the mapping.
func (m Mapping) HumanLabel(technicalServerID string) string {
	parts := strings.Split(technicalServerID, "#")
	if len(parts) != 3 {
		return technicalServerID
	}
	provider := parts[0]
	regionKey := parts[1]
	identifier := parts[2]

	for gameID, game := range m.Games {
		if game.Provider != provider {
			continue
		}
		region, ok := game.Regions[regionKey]
		if !ok {
			continue
		}
		for serverKey, server := range region.Servers {
			if fmt.Sprint(server.Identifier) == identifier {
				return DisplayLabel(gameID, regionKey, serverKey)
			}
		}
	}
	return technicalServerID
}
