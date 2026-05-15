package discordbot

import (
	"context"
	"os"

	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

func (h *Handler) loadServerMapping(ctx context.Context) (servermap.Mapping, error) {
	return h.mappingCache.Get(ctx, h.loadServerMappingFromS3)
}

func (h *Handler) loadServerMappingFromS3(ctx context.Context) (servermap.Mapping, error) {
	var mapping servermap.Mapping

	bucket := os.Getenv("CONFIG_BUCKET")
	if bucket == "" {
		bucket = defaultConfigBucket
	}
	key := os.Getenv("SERVER_MAPPING_PATH")
	if key == "" {
		key = defaultServerMappingKey
	}

	if err := h.configProvider.LoadJSONFromS3(ctx, bucket, key, &mapping); err != nil {
		return servermap.Mapping{}, err
	}
	return mapping, nil
}
