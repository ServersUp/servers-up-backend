// Package scope defines wildcard subscription scopes ("subscribe to ALL").
//
// A subscription targets either a single server (today's behavior) or a
// wildcard scope: every server of a game in one region ("region") or every
// server of a game worldwide ("game"). Wildcard subscriptions are stored in
// the same subscriptions table using reserved partition keys that never
// collide with concrete server IDs ("provider#region#identifier").
package scope

import (
	"fmt"
	"strings"
)

// Subscription scope types.
const (
	TypeRegion = "region"
	TypeGame   = "game"
)

// Aggregate scope states.
const (
	StateAllUp   = "ALL_UP"
	StateMixed   = "MIXED"
	StateAllDown = "ALL_DOWN"
)

// Key returns the subscription partition key for a wildcard scope.
//
//	region scope: region#<gameId>#<region>   (e.g. region#wow#eu)
//	game scope:   game#<gameId>              (e.g. game#wow)
//
// Concrete server IDs always start with a provider (e.g. "battlenet#us#11"),
// so wildcard keys can never collide with them.
func Key(gameID, region string) string {
	gameID = strings.ToLower(strings.TrimSpace(gameID))
	if region == "" {
		return fmt.Sprintf("%s#%s", TypeGame, gameID)
	}
	return fmt.Sprintf("%s#%s#%s", TypeRegion, gameID, strings.ToLower(strings.TrimSpace(region)))
}

// TypeOf returns the scope type for a wildcard key, or "" if the key is not a
// region/game wildcard (i.e. a concrete server ID).
func TypeOf(key string) string {
	switch {
	case strings.HasPrefix(key, TypeRegion+"#"):
		return TypeRegion
	case strings.HasPrefix(key, TypeGame+"#"):
		return TypeGame
	}
	return ""
}

// IsWildcard reports whether key is a region/game wildcard scope key.
func IsWildcard(key string) bool {
	return TypeOf(key) != ""
}

// GameID extracts the normalized game ID from a wildcard key.
func GameID(key string) string {
	switch TypeOf(key) {
	case TypeRegion:
		if parts := strings.Split(key, "#"); len(parts) == 3 {
			return parts[1]
		}
	case TypeGame:
		if parts := strings.Split(key, "#"); len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

// Region extracts the region from a region-scope key ("" for game scope).
func Region(key string) string {
	if TypeOf(key) != TypeRegion {
		return ""
	}
	if parts := strings.Split(key, "#"); len(parts) == 3 {
		return parts[2]
	}
	return ""
}

// Label returns a human-readable label for a scope, e.g. "All WoW servers" or
// "All WoW servers — EU". Used for /subscriptions and /unsubscribe display.
func Label(gameID, region string) string {
	name := GameDisplayName(gameID)
	if region == "" {
		return fmt.Sprintf("All %s servers", name)
	}
	return fmt.Sprintf("All %s servers — %s", name, strings.ToUpper(strings.TrimSpace(region)))
}

// GameDisplayName returns the display name for a game ID ("wow" → "WoW").
func GameDisplayName(gameID string) string {
	switch strings.ToLower(strings.TrimSpace(gameID)) {
	case "wow":
		return "WoW"
	case "ffxiv":
		return "FFXIV"
	case "":
		return ""
	}
	id := strings.ToLower(strings.TrimSpace(gameID))
	return strings.ToUpper(id[:1]) + id[1:]
}
