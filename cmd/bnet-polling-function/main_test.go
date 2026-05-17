package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/bnet"
	"github.com/ServersUp/servers-up-backend/internal/db"
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
