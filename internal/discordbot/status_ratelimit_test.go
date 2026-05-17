package discordbot

import (
	"fmt"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/discord"
)

func TestStatusRateLimiter_AllowWithinWindow(t *testing.T) {
	t.Parallel()
	l := newStatusRateLimiter()
	ix := discord.Interaction{
		GuildID: "guild-1",
		Member:  &discord.InteractionMember{User: discord.InteractionUser{ID: "user-1"}},
	}
	for i := 0; i < statusPerUserLimit; i++ {
		allowed, _ := l.Allow(ix)
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	allowed, retry := l.Allow(ix)
	if allowed {
		t.Fatal("expected 6th request to be denied")
	}
	if retry <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", retry)
	}
}

func TestStatusRateLimiter_WindowReset(t *testing.T) {
	t.Parallel()
	l := newStatusRateLimiter()
	l.buckets["user:u1"] = windowBucket{
		windowStart: time.Now().Add(-statusRateWindow - time.Second),
		count:       statusPerUserLimit,
	}
	ix := discord.Interaction{
		Member: &discord.InteractionMember{User: discord.InteractionUser{ID: "u1"}},
	}
	allowed, _ := l.Allow(ix)
	if !allowed {
		t.Fatal("expected allow after window expired")
	}
}

func TestStatusRateLimiter_GuildLimit(t *testing.T) {
	t.Parallel()
	l := newStatusRateLimiter()
	ix := func(uid string) discord.Interaction {
		return discord.Interaction{
			GuildID: "guild-heavy",
			Member:  &discord.InteractionMember{User: discord.InteractionUser{ID: uid}},
		}
	}
	for i := 0; i < statusPerGuildLimit; i++ {
		allowed, _ := l.Allow(ix(fmt.Sprintf("user-%d", i)))
		if !allowed {
			t.Fatalf("guild request %d should be allowed", i+1)
		}
	}
	allowed, _ := l.Allow(ix("user-new"))
	if allowed {
		t.Fatal("expected guild limit to deny")
	}
}
