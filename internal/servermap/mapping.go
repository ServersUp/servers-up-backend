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
	Provider string `json:"provider"`
	// DisplayName is the optional user-facing name (e.g. "WoW Forever"). Its normalized form is
	// the slug users type and see; without it the game ID is used.
	DisplayName string            `json:"displayName,omitempty"`
	Regions     map[string]Region `json:"regions"`
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

// GameSlug returns the user-facing slug for a game ID: its normalized display name, or the ID.
func (m Mapping) GameSlug(gameID string) string {
	if k := NormalizeKey(m.Games[gameID].DisplayName); k != "" {
		return k
	}
	return gameID
}

// GameSlugs returns the user-facing slugs of all games, sorted.
func (m Mapping) GameSlugs() []string {
	out := make([]string, 0, len(m.Games))
	for id := range m.Games {
		out = append(out, m.GameSlug(id))
	}
	sort.Strings(out)
	return out
}

// ResolveGame maps user input (game ID, slug or display name) to a game ID. Unknown input is
// returned normalized so callers report it as an unknown game.
func (m Mapping) ResolveGame(input string) string {
	k := NormalizeKey(input)
	if _, ok := m.Games[k]; ok {
		return k
	}
	for id := range m.Games {
		if m.GameSlug(id) == k {
			return id
		}
	}
	return k
}

func (m Mapping) ListRegions(gameID string) ([]string, error) {
	gameID = m.ResolveGame(gameID)
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
	gameID = m.ResolveGame(gameID)
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
	gameID = m.ResolveGame(gameInput)
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
func (m Mapping) DisplayLabel(gameID, regionKey, serverKey string) string {
	return fmt.Sprintf("%s-%s-%s", m.GameSlug(gameID), regionKey, serverKey)
}

// Locate finds the game, region and server keys for a technical server ID (provider#region#identifier).
func (m Mapping) Locate(technicalServerID string) (gameID, regionKey, serverKey string, ok bool) {
	parts := strings.Split(technicalServerID, "#")
	if len(parts) != 3 {
		return "", "", "", false
	}
	for gameID, game := range m.Games {
		if game.Provider != parts[0] {
			continue
		}
		for serverKey, server := range game.Regions[parts[1]].Servers {
			if fmt.Sprint(server.Identifier) == parts[2] {
				return gameID, parts[1], serverKey, true
			}
		}
	}
	return "", "", "", false
}

// HumanLabel maps a technical server ID to a display label (game-region-server).
// Returns technicalServerID unchanged when the ID is malformed or not found in the mapping.
func (m Mapping) HumanLabel(technicalServerID string) string {
	if g, r, s, ok := m.Locate(technicalServerID); ok {
		return m.DisplayLabel(g, r, s)
	}
	return technicalServerID
}

// DuplicateServerIDs returns technical server IDs (provider#region#identifier) claimed by
// more than one game, formatted "id: gameA, gameB". The Subscriptions table is keyed on the
// technical ID alone, so a shared ID would deliver one game's alerts to another's subscribers.
func (m Mapping) DuplicateServerIDs() []string {
	// Several servers of one game may share an identifier (WoW connected realms), so
	// track the set of games per ID, not the number of servers.
	owners := map[string]map[string]struct{}{}
	for gameID, game := range m.Games {
		for regionKey, region := range game.Regions {
			for _, server := range region.Servers {
				id := fmt.Sprintf("%s#%s#%v", game.Provider, regionKey, server.Identifier)
				if owners[id] == nil {
					owners[id] = map[string]struct{}{}
				}
				owners[id][gameID] = struct{}{}
			}
		}
	}
	var out []string
	for id, set := range owners {
		if len(set) < 2 {
			continue
		}
		games := make([]string, 0, len(set))
		for g := range set {
			games = append(games, g)
		}
		sort.Strings(games)
		out = append(out, id+": "+strings.Join(games, ", "))
	}
	sort.Strings(out)
	return out
}
