package foreverpoller

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/db"
)

type fakeBnet struct {
	authErr error
	// by "region/id"; a missing key returns an error (like a 404).
	status map[string]string
}

func (f *fakeBnet) Authenticate(context.Context) error { return f.authErr }

func (f *fakeBnet) GetRulesetStatus(_ context.Context, region string, id int, _ string) (*bnet.RulesetResponse, error) {
	st, ok := f.status[region+"/"+string(rune('0'+id))]
	if !ok {
		return nil, errors.New("status 404")
	}
	return &bnet.RulesetResponse{ID: id, Status: bnet.Status{Type: st}}, nil
}

type save struct {
	game, provider, region string
	id                     any
	status                 string
}

type fakeDB struct {
	mu    sync.Mutex
	saves []save
	err   error
}

func (f *fakeDB) SaveServerStatus(_ context.Context, game, provider, region string, id any, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, save{game, provider, region, id, status})
	return f.err
}

func cfg() Config {
	return Config{Regions: map[string]RegionConfig{
		"us": {Locale: "en_US", Rulesets: []RulesetConfig{{Name: "PvP", Slug: "pvp", RulesetID: 1}, {Name: "Normal", Slug: "normal", RulesetID: 2}}},
		"kr": {Locale: "ko_KR", Rulesets: []RulesetConfig{{Name: "PvP", Slug: "pvp", RulesetID: 1}}},
	}}
}

func TestPoll_savesUnderForeverGameAndBattlenetProvider(t *testing.T) {
	t.Parallel()
	d := &fakeDB{}
	h := &Handler{database: d}
	c := &fakeBnet{status: map[string]string{"us/1": "UP", "us/2": "DOWN", "kr/1": "UP"}}

	s, err := h.poll(context.Background(), c, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if s.Successful != 3 || s.Changed != 3 || s.Up != 2 || s.Down != 1 || s.Errors != 0 || s.Rulesets != 3 {
		t.Fatalf("summary: %+v", s)
	}
	for _, sv := range d.saves {
		if sv.game != "forever" || sv.provider != "battlenet" {
			t.Fatalf("wrong game/provider: %+v", sv)
		}
	}
}

func TestPoll_fetchErrorIsNeverRecordedAsDown(t *testing.T) {
	t.Parallel()
	d := &fakeDB{}
	h := &Handler{database: d}
	c := &fakeBnet{status: map[string]string{"us/1": "UP"}} // us/2 and kr/1 "404"

	s, err := h.poll(context.Background(), c, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors != 2 || s.Down != 0 || len(d.saves) != 1 {
		t.Fatalf("summary %+v saves %+v", s, d.saves)
	}
}

func TestPoll_unexpectedStatusIsErrorNotSaved(t *testing.T) {
	t.Parallel()
	for _, st := range []string{"", "MAINTENANCE", "up"} {
		d := &fakeDB{}
		h := &Handler{database: d}
		c := &fakeBnet{status: map[string]string{"us/1": st, "us/2": st, "kr/1": st}}
		s, _ := h.poll(context.Background(), c, cfg())
		if s.Errors != 3 || len(d.saves) != 0 {
			t.Fatalf("status %q: summary %+v saves %+v", st, s, d.saves)
		}
	}
}

func TestPoll_unchangedCountsSuccessNotChanged(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{err: db.ErrStatusUnchanged}}
	c := &fakeBnet{status: map[string]string{"us/1": "UP", "us/2": "UP", "kr/1": "UP"}}
	s, _ := h.poll(context.Background(), c, cfg())
	if s.Successful != 3 || s.Changed != 0 || s.Errors != 0 {
		t.Fatalf("summary: %+v", s)
	}
}

func TestPoll_saveErrorCounts(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{err: errors.New("ddb down")}}
	c := &fakeBnet{status: map[string]string{"us/1": "UP", "us/2": "UP", "kr/1": "UP"}}
	s, _ := h.poll(context.Background(), c, cfg())
	if s.Errors != 3 || s.Successful != 0 {
		t.Fatalf("summary: %+v", s)
	}
}

func TestPoll_authFailureAborts(t *testing.T) {
	t.Parallel()
	d := &fakeDB{}
	h := &Handler{database: d}
	if _, err := h.poll(context.Background(), &fakeBnet{authErr: errors.New("bad creds")}, cfg()); err == nil {
		t.Fatal("expected auth error")
	}
	if len(d.saves) != 0 {
		t.Fatal("nothing should be saved")
	}
}

func TestNew_validatesDeps(t *testing.T) {
	t.Parallel()
	if _, err := New(Deps{}); err == nil {
		t.Fatal("expected error for empty deps")
	}
}
