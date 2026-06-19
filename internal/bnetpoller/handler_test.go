package bnetpoller

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/aws/aws-lambda-go/events"
)

type fakeBnet struct {
	authErr   error
	status    string
	statusErr error
}

func (f *fakeBnet) Authenticate(ctx context.Context) error {
	return f.authErr
}

func (f *fakeBnet) GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*bnet.ConnectedRealmResponse, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return &bnet.ConnectedRealmResponse{
		Status: bnet.Status{Type: f.status},
	}, nil
}

type fakeDB struct {
	saves atomic.Int32
	err   error
}

func (f *fakeDB) SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error {
	f.saves.Add(1)
	if f.err != nil {
		return f.err
	}
	return nil
}

func TestPollRealms_countsSuccessAndErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{
			{Name: "a", ConnectedRealmID: 1},
			{Name: "b", ConnectedRealmID: 2},
		},
	}

	summary, err := h.pollRealms(context.Background(), &fakeBnet{status: "UP"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Successful != 2 || summary.Up != 2 || summary.Errors != 0 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestPollRealms_apiErrorIncrementsErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{{Name: "a", ConnectedRealmID: 1}},
	}

	summary, err := h.pollRealms(context.Background(), &fakeBnet{statusErr: errors.New("api down")}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Errors != 1 || summary.Successful != 0 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestPollRealms_unchangedStatusStillSuccess(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{err: db.ErrStatusUnchanged}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{{Name: "a", ConnectedRealmID: 1}},
	}

	summary, err := h.pollRealms(context.Background(), &fakeBnet{status: "DOWN"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Successful != 1 || summary.Down != 1 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestPollRealms_authFailReturnsError(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{{Name: "a", ConnectedRealmID: 1}},
	}

	_, err := h.pollRealms(context.Background(), &fakeBnet{authErr: errors.New("auth failed")}, cfg)
	if err == nil {
		t.Fatal("expected error from authenticate failure")
	}
}

func TestPollRealms_dbSaveErrorIncrementsErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{err: errors.New("dynamodb error")}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{{Name: "a", ConnectedRealmID: 1}},
	}

	summary, err := h.pollRealms(context.Background(), &fakeBnet{status: "UP"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Errors != 1 || summary.Successful != 0 {
		t.Fatalf("expected 1 error and 0 success for non-ErrStatusUnchanged DB error, got %+v", summary)
	}
}

func TestPollRealms_statusTaxonomy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status     string
		wantUp     int32
		wantDown   int32
		wantOther  int32
	}{
		{"UP", 1, 0, 0},
		{"DOWN", 0, 1, 0},
		{"MAINTENANCE", 0, 0, 1},
		{"", 0, 0, 1},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			h := &Handler{database: &fakeDB{}}
			cfg := bnet.Config{
				Region: "us",
				Realms: []bnet.RealmConfig{{Name: "r", ConnectedRealmID: 1}},
			}
			summary, err := h.pollRealms(context.Background(), &fakeBnet{status: tc.status}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Up != tc.wantUp {
				t.Errorf("Up: got %d want %d", summary.Up, tc.wantUp)
			}
			if summary.Down != tc.wantDown {
				t.Errorf("Down: got %d want %d", summary.Down, tc.wantDown)
			}
			other := summary.Successful - summary.Up - summary.Down
			if other != tc.wantOther {
				t.Errorf("other: got %d want %d (summary=%+v)", other, tc.wantOther, summary)
			}
		})
	}
}

// blockingFakeBnet is a bnetClient that blocks GetConnectedRealmStatus until ctx is done.
type blockingFakeBnet struct{}

func (b *blockingFakeBnet) Authenticate(ctx context.Context) error { return nil }

func (b *blockingFakeBnet) GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*bnet.ConnectedRealmResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPollRealms_contextCancel_doesNotHang(t *testing.T) {
	t.Parallel()

	h := &Handler{database: &fakeDB{}}
	realms := make([]bnet.RealmConfig, 5)
	for i := range realms {
		realms[i] = bnet.RealmConfig{Name: fmt.Sprintf("r%d", i), ConnectedRealmID: i + 1}
	}
	cfg := bnet.Config{Region: "us", Locale: "en_US", Realms: realms}

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		summary pollSummary
		err     error
	}
	done := make(chan result, 1)
	go func() {
		s, err := h.pollRealms(ctx, &blockingFakeBnet{}, cfg)
		done <- result{s, err}
	}()

	// Give goroutines a moment to start blocking, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// pollRealms completed after cancel — pass.
	case <-time.After(2 * time.Second):
		t.Fatal("pollRealms did not complete within 2 s after context cancel")
	}
}

type fakeConfigLoader struct {
	cfg        bnet.Config
	err        error
	gotBucket  string
	gotKey     string
}

func (f *fakeConfigLoader) LoadJSONFromS3(_ context.Context, bucket, key string, target any) error {
	f.gotBucket = bucket
	f.gotKey = key
	if f.err != nil {
		return f.err
	}
	*(target.(*bnet.Config)) = f.cfg
	return nil
}

func TestHandleRequest_usesInjectedConfig(t *testing.T) {
	t.Parallel()
	fakeConfig := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{{Name: "a", ConnectedRealmID: 1}},
	}
	loader := &fakeConfigLoader{cfg: fakeConfig}
	h := &Handler{
		configBucket:   "bucket",
		configKey:      "key",
		configProvider: loader,
		database:       &fakeDB{},
		bnetClientID:   "id",
		bnetSecret:     "secret",
	}
	// Swap the bnet client creation to avoid real HTTP; we test config wiring only.
	// pollRealms is tested separately; here we verify HandleRequest loads config and returns no error.
	// We use a minimal fakeBnet by overriding the client via a thin wrapper.
	_, err := h.handleRequestWithClient(context.Background(), events.CloudWatchEvent{ID: "test"}, &fakeBnet{status: "UP"})
	if err != nil {
		t.Fatal(err)
	}
	if loader.gotBucket != "bucket" || loader.gotKey != "key" {
		t.Fatalf("config loader args: bucket=%q key=%q", loader.gotBucket, loader.gotKey)
	}
}

func TestHandleRequest_configLoadError(t *testing.T) {
	t.Parallel()
	h := &Handler{
		configBucket:   "bucket",
		configKey:      "key",
		configProvider: &fakeConfigLoader{err: errors.New("s3 unavailable")},
		database:       &fakeDB{},
	}
	_, err := h.handleRequestWithClient(context.Background(), events.CloudWatchEvent{ID: "test"}, &fakeBnet{})
	if err == nil {
		t.Fatal("expected error from config load failure")
	}
}

func TestPollRealms_recordsTiming(t *testing.T) {
	t.Parallel()

	h := &Handler{database: &fakeDB{}}
	cfg := bnet.Config{
		Region: "us",
		Locale: "en_US",
		Realms: []bnet.RealmConfig{
			{Name: "a", ConnectedRealmID: 1},
			{Name: "b", ConnectedRealmID: 2},
		},
	}

	summary, err := h.pollRealms(context.Background(), &slowFakeBnet{delay: 5 * time.Millisecond, status: "UP"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.BnetCalls != 2 || summary.DdbCalls != 2 {
		t.Fatalf("calls: bnet=%d ddb=%d", summary.BnetCalls, summary.DdbCalls)
	}
	if summary.BnetMaxMs < 5 || summary.PollDurationMs < 5 {
		t.Fatalf("expected non-zero timing, got %+v", summary)
	}
	if summary.BnetTotalMs < summary.BnetMaxMs {
		t.Fatalf("bnet total should be >= max, got %+v", summary)
	}
}

type slowFakeBnet struct {
	delay  time.Duration
	status string
}

func (f *slowFakeBnet) Authenticate(ctx context.Context) error { return nil }

func (f *slowFakeBnet) GetConnectedRealmStatus(ctx context.Context, region string, connectedRealmID int, locale string) (*bnet.ConnectedRealmResponse, error) {
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	return &bnet.ConnectedRealmResponse{Status: bnet.Status{Type: f.status}}, nil
}

func TestNew(t *testing.T) {
	t.Parallel()

	valid := Deps{
		ConfigLoader:     &fakeConfigLoader{},
		StatusDB:         &fakeDB{},
		BnetClientID:     "id",
		BnetClientSecret: "secret",
		ConfigBucket:     "bucket",
		ConfigKey:        "key",
	}

	tests := []struct {
		name    string
		deps    Deps
		wantErr bool
	}{
		{name: "success", deps: valid, wantErr: false},
		{
			name:    "nil ConfigLoader",
			deps:    func() Deps { d := valid; d.ConfigLoader = nil; return d }(),
			wantErr: true,
		},
		{
			name:    "nil StatusDB",
			deps:    func() Deps { d := valid; d.StatusDB = nil; return d }(),
			wantErr: true,
		},
		{
			name:    "empty BnetClientID",
			deps:    func() Deps { d := valid; d.BnetClientID = ""; return d }(),
			wantErr: true,
		},
		{
			name:    "empty BnetClientSecret",
			deps:    func() Deps { d := valid; d.BnetClientSecret = ""; return d }(),
			wantErr: true,
		},
		{
			name:    "empty ConfigBucket",
			deps:    func() Deps { d := valid; d.ConfigBucket = ""; return d }(),
			wantErr: true,
		},
		{
			name:    "empty ConfigKey",
			deps:    func() Deps { d := valid; d.ConfigKey = ""; return d }(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := New(tt.deps)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h == nil {
				t.Fatal("expected non-nil Handler")
			}
		})
	}
}
