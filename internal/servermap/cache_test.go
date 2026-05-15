package servermap

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedMapping_usesTTL(t *testing.T) {
	t.Parallel()
	var loads atomic.Int32
	cache := NewCachedMapping(100 * time.Millisecond)
	load := func(ctx context.Context) (Mapping, error) {
		loads.Add(1)
		return Mapping{Games: map[string]Game{"g": {}}}, nil
	}

	ctx := context.Background()
	if _, err := cache.Get(ctx, load); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(ctx, load); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 1 {
		t.Fatalf("expected 1 load within TTL, got %d", loads.Load())
	}

	time.Sleep(110 * time.Millisecond)
	if _, err := cache.Get(ctx, load); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatalf("expected reload after TTL, got %d loads", loads.Load())
	}
}

func TestCachedMapping_propagatesLoadError(t *testing.T) {
	t.Parallel()
	cache := NewCachedMapping(time.Minute)
	_, err := cache.Get(context.Background(), func(ctx context.Context) (Mapping, error) {
		return Mapping{}, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
