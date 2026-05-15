package servermap

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"
)

const defaultMappingCacheTTL = 60 * time.Second

// MappingLoader fetches a server mapping (e.g. from S3).
type MappingLoader func(ctx context.Context) (Mapping, error)

// CachedMapping stores a mapping with a TTL so config updates propagate without pinning forever.
type CachedMapping struct {
	mu       sync.RWMutex
	m        Mapping
	loadedAt time.Time
	ttl      time.Duration
}

// NewCachedMapping returns a cache with the given TTL. Non-positive TTL uses defaultMappingCacheTTL.
func NewCachedMapping(ttl time.Duration) *CachedMapping {
	if ttl <= 0 {
		ttl = defaultMappingCacheTTL
	}
	return &CachedMapping{ttl: ttl}
}

// CacheTTLFromEnv reads MAPPING_CACHE_TTL_SECONDS (default 60).
func CacheTTLFromEnv() time.Duration {
	raw := os.Getenv("MAPPING_CACHE_TTL_SECONDS")
	if raw == "" {
		return defaultMappingCacheTTL
	}
	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		return defaultMappingCacheTTL
	}
	return time.Duration(sec) * time.Second
}

// Seed sets the cached mapping as if just loaded (for tests).
func (c *CachedMapping) Seed(m Mapping) {
	c.mu.Lock()
	c.m = m
	c.loadedAt = time.Now()
	c.mu.Unlock()
}

// Get returns the cached mapping or loads it via load when stale or empty.
func (c *CachedMapping) Get(ctx context.Context, load MappingLoader) (Mapping, error) {
	c.mu.RLock()
	if !c.loadedAt.IsZero() && time.Since(c.loadedAt) < c.ttl {
		m := c.m
		c.mu.RUnlock()
		return m, nil
	}
	c.mu.RUnlock()

	m, err := load(ctx)
	if err != nil {
		return Mapping{}, err
	}

	c.mu.Lock()
	c.m = m
	c.loadedAt = time.Now()
	c.mu.Unlock()
	return m, nil
}
