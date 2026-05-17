package discordbot

import (
	"sync"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
)

const statusResultCacheTTL = 45 * time.Second

type statusCacheEntry struct {
	row *models.GameServerStatus
	at  time.Time
}

type statusResultCache struct {
	mu      sync.RWMutex
	entries map[string]statusCacheEntry
}

func newStatusResultCache() *statusResultCache {
	return &statusResultCache{
		entries: make(map[string]statusCacheEntry),
	}
}

func statusCacheKey(gameID, serverID string) string {
	return gameID + "#" + serverID
}

func (c *statusResultCache) Get(gameID, serverID string) (*models.GameServerStatus, bool) {
	key := statusCacheKey(gameID, serverID)
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(entry.at) >= statusResultCacheTTL {
		return nil, false
	}
	return entry.row, true
}

func (c *statusResultCache) Set(gameID, serverID string, row *models.GameServerStatus) {
	if row == nil {
		return
	}
	copy := *row
	c.mu.Lock()
	c.entries[statusCacheKey(gameID, serverID)] = statusCacheEntry{
		row: &copy,
		at:  time.Now(),
	}
	c.mu.Unlock()
}
