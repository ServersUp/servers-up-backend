// Package scope defines wildcard subscription scopes ("subscribe to ALL").
//
// A subscription targets either a single server (today's behavior) or a
// wildcard scope: every server of a game in one region ("region"). Wildcard
// subscriptions are stored in the same subscriptions table using reserved
// partition keys that never collide with concrete server IDs
// ("provider#region#identifier").
package scope

import (
	"fmt"
	"strings"
)

// Subscription scope types.
const (
	TypeRegion = "region"
)

// Aggregate scope states.
const (
	StateAllUp   = "ALL_UP"
	StateMixed   = "MIXED"
	StateAllDown = "ALL_DOWN"
)

// Key returns the subscription partition key for a wildcard scope. A wildcard
// scope always requires a region (the minimum subscription is a game + region):
//
//	region scope: region#<gameId>#<region>   (e.g. region#wow#eu)
//
// Concrete server IDs always start with a provider (e.g. "battlenet#us#11"),
// so wildcard keys can never collide with them.
func Key(gameID, region string) string {
	gameID = strings.ToLower(strings.TrimSpace(gameID))
	if strings.TrimSpace(region) == "" {
		return ""
	}
	return fmt.Sprintf("%s#%s#%s", TypeRegion, gameID, strings.ToLower(strings.TrimSpace(region)))
}

// TypeOf returns the scope type for a wildcard key, or "" if the key is not a
// region wildcard (i.e. a concrete server ID).
func TypeOf(key string) string {
	if strings.HasPrefix(key, TypeRegion+"#") {
		return TypeRegion
	}
	return ""
}

// IsWildcard reports whether key is a region wildcard scope key.
func IsWildcard(key string) bool {
	return TypeOf(key) != ""
}

// GameID extracts the normalized game ID from a wildcard key.
func GameID(key string) string {
	if TypeOf(key) != TypeRegion {
		return ""
	}
	if parts := strings.Split(key, "#"); len(parts) == 3 {
		return parts[1]
	}
	return ""
}

// Region extracts the region from a region-scope key ("" for non-region keys).
func Region(key string) string {
	if TypeOf(key) != TypeRegion {
		return ""
	}
	if parts := strings.Split(key, "#"); len(parts) == 3 {
		return parts[2]
	}
	return ""
}

// Label returns a human-readable label for a region scope, e.g. "All WoW
// servers — EU". Used for /subscriptions and /unsubscribe display.
func Label(gameID, region string) string {
	name := GameDisplayName(gameID)
	if strings.TrimSpace(region) == "" {
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
