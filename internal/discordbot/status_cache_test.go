package discordbot

import (
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
)

func TestStatusResultCache_GetSetTTL(t *testing.T) {
	t.Parallel()
	c := newStatusResultCache()
	row := &models.GameServerStatus{GameID: "wow", ServerID: "battlenet#us#57", Status: "UP"}
	c.Set("wow", "battlenet#us#57", row)

	got, ok := c.Get("wow", "battlenet#us#57")
	if !ok || got.Status != "UP" {
		t.Fatalf("expected cache hit, got ok=%v row=%#v", ok, got)
	}

	c.mu.Lock()
	key := statusCacheKey("wow", "battlenet#us#57")
	entry := c.entries[key]
	entry.at = time.Now().Add(-statusResultCacheTTL - time.Second)
	c.entries[key] = entry
	c.mu.Unlock()

	_, ok = c.Get("wow", "battlenet#us#57")
	if ok {
		t.Fatal("expected cache miss after TTL")
	}
}
