// Package aggregate computes the aggregate state of a wildcard subscription
// scope ("subscribe to ALL"). A scope is ALL_UP when every catalog member is
// UP, ALL_DOWN when none is UP, and MIXED otherwise. Catalog members without a
// status row (or with any non-UP status) count as not-UP so that partial data
// coverage can never falsely reach ALL_UP.
package aggregate

import (
	"fmt"

	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

// Episode returns the once-per-episode token used to guard aggregate
// notifications. stateSince is set when a terminal state is entered, so
// "{state}:{stateSince}" uniquely identifies a terminal episode.
func Episode(state string, stateSince int64) string {
	return fmt.Sprintf("%s:%d", state, stateSince)
}

// DeriveState returns the aggregate state for a scope given its totals.
func DeriveState(totalCount, upCount int) string {
	if totalCount <= 0 {
		return scope.StateMixed
	}
	switch {
	case upCount == totalCount:
		return scope.StateAllUp
	case upCount == 0:
		return scope.StateAllDown
	default:
		return scope.StateMixed
	}
}

// IsTerminal reports whether state is a terminal (notifiable) aggregate state.
func IsTerminal(state string) bool {
	return state == scope.StateAllUp || state == scope.StateAllDown
}

// Counts computes the number of catalog members (total) and the number of
// catalog members currently UP for a scope. statusByServer maps concrete server
// IDs (serverid.Generate output) to their status; members absent from the map
// or with a non-UP status count as not-UP. region is the required region of
// the scope (game + region is the minimum wildcard subscription).
func Counts(mapping servermap.Mapping, statusByServer map[string]string, gameID, region string) (total, up int) {
	gameID = servermap.NormalizeKey(gameID)
	region = servermap.NormalizeKey(region)

	game, ok := mapping.Games[gameID]
	if !ok {
		return 0, 0
	}
	provider := game.Provider
	for regionKey, reg := range game.Regions {
		if region != "" && regionKey != region {
			continue
		}
		for _, srv := range reg.Servers {
			total++
			if statusByServer[serverid.Generate(provider, regionKey, srv.Identifier)] == "UP" {
				up++
			}
		}
	}
	return total, up
}
