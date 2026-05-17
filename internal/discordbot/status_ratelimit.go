package discordbot

import (
	"strings"
	"sync"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/discord"
)

const (
	statusPerUserLimit   = 5
	statusPerGuildLimit  = 30
	statusRateWindow     = time.Minute
	statusRateMaxBuckets = 10_000
)

type windowBucket struct {
	windowStart time.Time
	count       int
}

type statusRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]windowBucket
}

func newStatusRateLimiter() *statusRateLimiter {
	return &statusRateLimiter{
		buckets: make(map[string]windowBucket),
	}
}

// Allow reports whether /status may proceed and how long to wait when denied.
func (l *statusRateLimiter) Allow(interaction discord.Interaction) (allowed bool, retryAfter time.Duration) {
	now := time.Now()
	keys := l.keysFor(interaction)
	if len(keys) == 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupExpiredLocked(now)

	allowed = true
	var maxRetry time.Duration
	for _, key := range keys {
		limit := limitForRateKey(key)
		bucket := l.buckets[key]
		if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= statusRateWindow {
			continue
		}
		if bucket.count >= limit {
			retry := statusRateWindow - now.Sub(bucket.windowStart)
			if retry > maxRetry {
				maxRetry = retry
			}
			allowed = false
		}
	}
	if !allowed {
		if maxRetry < time.Second {
			maxRetry = time.Second
		}
		return false, maxRetry
	}

	for _, key := range keys {
		bucket := l.buckets[key]
		if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= statusRateWindow {
			bucket = windowBucket{windowStart: now, count: 0}
		}
		bucket.count++
		l.buckets[key] = bucket
	}
	return true, 0
}

func (l *statusRateLimiter) keysFor(interaction discord.Interaction) []string {
	var keys []string
	if uid := interaction.InvokerUserID(); uid != "" {
		keys = append(keys, "user:"+uid)
	} else if interaction.GuildID != "" {
		keys = append(keys, "guild:"+interaction.GuildID)
	} else if interaction.ChannelID != "" {
		keys = append(keys, "channel:"+interaction.ChannelID)
	}
	if interaction.GuildID != "" && interaction.InvokerUserID() != "" {
		keys = append(keys, "guild:"+interaction.GuildID)
	}
	return keys
}

func limitForRateKey(key string) int {
	switch {
	case strings.HasPrefix(key, "user:"):
		return statusPerUserLimit
	case strings.HasPrefix(key, "guild:"):
		return statusPerGuildLimit
	default:
		return statusPerUserLimit
	}
}

func (l *statusRateLimiter) cleanupExpiredLocked(now time.Time) {
	for key, bucket := range l.buckets {
		if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= statusRateWindow {
			delete(l.buckets, key)
		}
	}
	for len(l.buckets) > statusRateMaxBuckets {
		for key := range l.buckets {
			delete(l.buckets, key)
			if len(l.buckets) <= statusRateMaxBuckets {
				break
			}
		}
	}
}
