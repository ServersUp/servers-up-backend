package statussnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	StatusUnknown      = "UNKNOWN"
	cacheControlPublic = "public, max-age=30"
	contentTypeJSON    = "application/json"
)

type configLoader interface {
	LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error
}

type statusLister interface {
	ListServerStatusesByGame(ctx context.Context, gameID string) ([]models.GameServerStatus, error)
}

type objectPutter interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Snapshot is the public JSON document published for the status page CDN.
type Snapshot struct {
	GeneratedAt int64                `json:"generatedAt"`
	Games       map[string]GameSnap  `json:"games"`
}

type GameSnap struct {
	Regions map[string]RegionSnap `json:"regions"`
}

type RegionSnap struct {
	Servers []ServerSnap `json:"servers"`
}

type ServerSnap struct {
	Slug          string `json:"slug"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	LastUpdatedAt int64  `json:"lastUpdatedAt"`
}

// Deps are injectable dependencies for the publisher Handler.
type Deps struct {
	ConfigLoader     configLoader
	StatusDB         statusLister
	S3               objectPutter
	ConfigBucket     string
	ServerMappingKey string
	SnapshotBucket   string
	SnapshotKey      string
	Now              func() time.Time
}

// Handler publishes a public status snapshot to S3.
type Handler struct {
	configLoader     configLoader
	statusDB         statusLister
	s3               objectPutter
	configBucket     string
	serverMappingKey string
	snapshotBucket   string
	snapshotKey      string
	now              func() time.Time
}

// New validates Deps and returns a Handler.
func New(d Deps) (*Handler, error) {
	if d.ConfigLoader == nil {
		return nil, fmt.Errorf("statussnapshot: ConfigLoader is required")
	}
	if d.StatusDB == nil {
		return nil, fmt.Errorf("statussnapshot: StatusDB is required")
	}
	if d.S3 == nil {
		return nil, fmt.Errorf("statussnapshot: S3 is required")
	}
	if d.ConfigBucket == "" || d.ServerMappingKey == "" {
		return nil, fmt.Errorf("statussnapshot: ConfigBucket and ServerMappingKey are required")
	}
	if d.SnapshotBucket == "" || d.SnapshotKey == "" {
		return nil, fmt.Errorf("statussnapshot: SnapshotBucket and SnapshotKey are required")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		configLoader:     d.ConfigLoader,
		statusDB:         d.StatusDB,
		s3:               d.S3,
		configBucket:     d.ConfigBucket,
		serverMappingKey: d.ServerMappingKey,
		snapshotBucket:   d.SnapshotBucket,
		snapshotKey:      d.SnapshotKey,
		now:              now,
	}, nil
}

// HandleRequest builds the public snapshot from mapping + DynamoDB and writes it to S3.
func (h *Handler) HandleRequest(ctx context.Context) error {
	var mapping servermap.Mapping
	if err := h.configLoader.LoadJSONFromS3(ctx, h.configBucket, h.serverMappingKey, &mapping); err != nil {
		return fmt.Errorf("statussnapshot: load server mapping: %w", err)
	}

	snap, err := BuildSnapshot(ctx, mapping, h.statusDB, h.now().Unix())
	if err != nil {
		return err
	}

	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("statussnapshot: marshal snapshot: %w", err)
	}

	_, err = h.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               aws.String(h.snapshotBucket),
		Key:                  aws.String(h.snapshotKey),
		Body:                 bytes.NewReader(body),
		ContentType:          aws.String(contentTypeJSON),
		CacheControl:         aws.String(cacheControlPublic),
		ServerSideEncryption: types.ServerSideEncryptionAes256,
	})
	if err != nil {
		return fmt.Errorf("statussnapshot: put snapshot object: %w", err)
	}

	slog.Info("published status snapshot",
		"bucket", h.snapshotBucket,
		"key", h.snapshotKey,
		"bytes", len(body),
		"games", len(snap.Games),
	)
	return nil
}

// BuildSnapshot joins server-mapping catalog rows with DynamoDB status by technical serverId.
func BuildSnapshot(ctx context.Context, mapping servermap.Mapping, statusDB statusLister, generatedAt int64) (*Snapshot, error) {
	snap := &Snapshot{
		GeneratedAt: generatedAt,
		Games:       make(map[string]GameSnap),
	}

	for _, gameID := range mapping.ListGames() {
		game := mapping.Games[gameID]
		rows, err := statusDB.ListServerStatusesByGame(ctx, gameID)
		if err != nil {
			return nil, fmt.Errorf("statussnapshot: list statuses for %s: %w", gameID, err)
		}
		byServerID := make(map[string]models.GameServerStatus, len(rows))
		for _, row := range rows {
			byServerID[row.ServerID] = row
		}

		regions := make(map[string]RegionSnap)
		regionKeys, err := mapping.ListRegions(gameID)
		if err != nil {
			return nil, err
		}
		for _, regionKey := range regionKeys {
			slugs, err := mapping.ListServers(gameID, regionKey)
			if err != nil {
				return nil, err
			}
			servers := make([]ServerSnap, 0, len(slugs))
			for _, slug := range slugs {
				srv := game.Regions[regionKey].Servers[slug]
				techID := serverid.Generate(game.Provider, regionKey, srv.Identifier)
				status := StatusUnknown
				var lastUpdated int64
				if row, ok := byServerID[techID]; ok {
					if row.Status != "" {
						status = row.Status
					}
					lastUpdated = row.LastUpdatedAt
				}
				servers = append(servers, ServerSnap{
					Slug:          slug,
					Label:         servermap.DisplayLabel(gameID, regionKey, slug),
					Status:        status,
					LastUpdatedAt: lastUpdated,
				})
			}
			sort.Slice(servers, func(i, j int) bool { return servers[i].Slug < servers[j].Slug })
			regions[regionKey] = RegionSnap{Servers: servers}
		}
		snap.Games[gameID] = GameSnap{Regions: regions}
	}

	return snap, nil
}
