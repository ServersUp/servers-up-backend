package discordbot

import (
	"context"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/aggregate"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

// gameStatusLister reads current status rows for a game.
type gameStatusLister interface {
	ListServerStatusesByGame(ctx context.Context, gameID string) ([]models.GameServerStatus, error)
}

// scopeStateStore persists aggregate scope state.
type scopeStateStore interface {
	Get(ctx context.Context, scopeKey string) (*models.ScopeState, error)
	Put(ctx context.Context, st models.ScopeState) error
	Delete(ctx context.Context, scopeKey string) error
}

// ensureScopeBaseline creates the ScopeState row for a wildcard scope on first
// subscribe, computing counts from the catalog and current statuses so the
// current state never triggers a notification. Rows already present are left
// untouched (the aggregator owns them afterwards).
func (h *Handler) ensureScopeBaseline(ctx context.Context, gameID, region, scopeKey string, mapping servermap.Mapping) error {
	existing, err := h.scopeStates.Get(ctx, scopeKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	statusByServer := map[string]string{}
	if h.gameStatuses != nil {
		rows, err := h.gameStatuses.ListServerStatusesByGame(ctx, gameID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			statusByServer[row.ServerID] = row.Status
		}
	}

	total, up := aggregate.Counts(mapping, statusByServer, gameID, region)
	now := time.Now().Unix()
	state := aggregate.DeriveState(total, up)
	st := models.ScopeState{
		ScopeKey:            scopeKey,
		UpCount:             up,
		DownCount:           total - up,
		TotalCount:          total,
		State:               state,
		StateSince:          now,
		LastNotifiedEpisode: aggregate.Episode(state, now),
		UpdatedAt:           now,
	}
	return h.scopeStates.Put(ctx, st)
}

// deleteScopeStateIfEmpty removes the ScopeState row for a wildcard key when no
// subscriptions remain for it.
func (h *Handler) deleteScopeStateIfEmpty(ctx context.Context, scopeKey string) error {
	subs, err := h.database.ListSubscriptionsByServer(ctx, scopeKey)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		if err := h.scopeStates.Delete(ctx, scopeKey); err != nil {
			return err
		}
	}
	return nil
}
