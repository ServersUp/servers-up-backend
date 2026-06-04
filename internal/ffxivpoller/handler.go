package ffxivpoller

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/ffxivlodestone"
	"github.com/ServersUp/servers-up-backend/internal/metrics"
	"github.com/aws/aws-lambda-go/events"
)

type statusDB interface {
	SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error
}

type configLoader interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

type statusSource interface {
	LoadStatusByWorldName(ctx context.Context, cfg ffxivlodestone.Config) (map[string]string, string, error)
}

// Deps holds dependencies for the FFXIV polling handler.
type Deps struct {
	ConfigLoader configLoader
	StatusDB     statusDB
	ConfigBucket string
	ConfigKey    string
	StatusSource statusSource
	HTTPClient   *http.Client
}

// Handler polls FFXIV world status and writes to DynamoDB.
type Handler struct {
	configBucket   string
	configKey      string
	configProvider configLoader
	database       statusDB
	statusSource   statusSource
}

// New constructs a Handler from resolved dependencies.
func New(deps Deps) (*Handler, error) {
	if deps.ConfigLoader == nil {
		return nil, errors.New("ffxivpoller: ConfigLoader is required")
	}
	if deps.StatusDB == nil {
		return nil, errors.New("ffxivpoller: StatusDB is required")
	}
	if deps.ConfigBucket == "" || deps.ConfigKey == "" {
		return nil, errors.New("ffxivpoller: ConfigBucket and ConfigKey are required")
	}
	src := deps.StatusSource
	if src == nil {
		src = &defaultStatusSource{client: deps.HTTPClient}
	}
	return &Handler{
		configBucket:   deps.ConfigBucket,
		configKey:      deps.ConfigKey,
		configProvider: deps.ConfigLoader,
		database:       deps.StatusDB,
		statusSource:   src,
	}, nil
}

func (h *Handler) HandleRequest(ctx context.Context, event events.CloudWatchEvent) (string, error) {
	slog.Info("Starting FFXIV polling execution", "eventID", event.ID)

	var pollerCfg ffxivlodestone.Config
	if err := h.configProvider.LoadJSONFromS3(ctx, h.configBucket, h.configKey, &pollerCfg); err != nil {
		return "", err
	}

	worlds, err := ffxivlodestone.ListCatalogWorlds(pollerCfg)
	if err != nil {
		return "", err
	}

	statusByName, source, err := h.statusSource.LoadStatusByWorldName(ctx, pollerCfg)
	if err != nil {
		return "", err
	}

	summary := h.applyStatuses(ctx, worlds, statusByName)

	slog.Info("FFXIV polling summary",
		"source", source,
		"catalogWorlds", len(worlds),
		"successful", summary.Successful,
		"up", summary.Up,
		"down", summary.Down,
		"errors", summary.Errors,
	)

	dims := map[string]string{"gameId": ffxivlodestone.GameID, "statusSource": source}
	metrics.EmitCount(metrics.Namespace, "PollRealmSuccess", dims, int64(summary.Successful))
	if summary.Errors > 0 {
		metrics.EmitCount(metrics.Namespace, "PollRealmError", dims, int64(summary.Errors))
	}

	return "Polling completed successfully", nil
}

type pollSummary struct {
	Successful int
	Up         int
	Down       int
	Errors     int
}

func (h *Handler) applyStatuses(ctx context.Context, worlds []ffxivlodestone.CatalogWorld, statusByName map[string]string) pollSummary {
	var summary pollSummary
	for _, w := range worlds {
		status, ok := statusByName[w.Name]
		if !ok {
			slog.Error("world missing from status feed", "world", w.Name, "region", w.Region)
			summary.Errors++
			continue
		}
		if status == "UP" {
			summary.Up++
		} else if status == "DOWN" {
			summary.Down++
		}

		err := h.database.SaveServerStatus(ctx, ffxivlodestone.GameID, ffxivlodestone.Provider, w.Region, w.Name, status)
		if err != nil {
			if errors.Is(err, db.ErrStatusUnchanged) {
				summary.Successful++
				continue
			}
			slog.Error("failed to save world status", "world", w.Name, "region", w.Region, "error", err)
			summary.Errors++
			continue
		}
		summary.Successful++
	}
	return summary
}

type defaultStatusSource struct {
	client *http.Client
}

func (s *defaultStatusSource) LoadStatusByWorldName(ctx context.Context, cfg ffxivlodestone.Config) (map[string]string, string, error) {
	lodestoneURL, frontierURL := cfg.ResolvedURLs()

	body, err := ffxivlodestone.FetchFrontierStatus(ctx, frontierURL, s.client)
	if err != nil {
		slog.Warn("frontier status fetch failed, falling back to Lodestone HTML", "error", err)
		return s.loadFromHTML(ctx, lodestoneURL)
	}

	codes, err := ffxivlodestone.ParseFrontierStatusJSON(body)
	if err != nil {
		slog.Warn("frontier status parse failed, falling back to Lodestone HTML", "error", err)
		return s.loadFromHTML(ctx, lodestoneURL)
	}

	out := make(map[string]string, len(codes))
	for name, code := range codes {
		status, err := ffxivlodestone.StatusFromFrontierCode(code)
		if err != nil {
			slog.Warn("skipping frontier entry with unknown code", "world", name, "code", code, "error", err)
			continue
		}
		out[name] = status
	}
	if len(out) == 0 {
		slog.Warn("frontier map empty after parsing, falling back to Lodestone HTML")
		return s.loadFromHTML(ctx, lodestoneURL)
	}
	return out, "frontier", nil
}

func (s *defaultStatusSource) loadFromHTML(ctx context.Context, lodestoneURL string) (map[string]string, string, error) {
	body, err := ffxivlodestone.FetchWorldStatus(ctx, lodestoneURL, s.client)
	if err != nil {
		return nil, "", err
	}
	entries, err := ffxivlodestone.ParseWorldStatusHTML(body)
	if err != nil {
		return nil, "", err
	}
	out, err := ffxivlodestone.StatusMapFromHTMLEntries(entries)
	if err != nil {
		return nil, "", err
	}
	return out, "lodestone", nil
}
