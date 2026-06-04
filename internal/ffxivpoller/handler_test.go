package ffxivpoller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/db"
	"github.com/ServersUp/servers-up-backend/internal/ffxivlodestone"
	"github.com/aws/aws-lambda-go/events"
)

type fakeConfigLoader struct {
	cfg ffxivlodestone.Config
	err error
}

func (f *fakeConfigLoader) LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error {
	if f.err != nil {
		return f.err
	}
	ptr, ok := target.(*ffxivlodestone.Config)
	if !ok {
		return errors.New("unexpected target type")
	}
	*ptr = f.cfg
	return nil
}

type fakeDB struct {
	saves int
	err   error
}

func (f *fakeDB) SaveServerStatus(ctx context.Context, gameID, provider, region string, identifier any, status string) error {
	f.saves++
	return f.err
}

type fakeStatusSource struct {
	byName map[string]string
	source string
	err    error
}

func (f *fakeStatusSource) LoadStatusByWorldName(ctx context.Context, cfg ffxivlodestone.Config) (map[string]string, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return f.byName, f.source, nil
}

func TestHandleRequest_savesWhenStatusesPresent(t *testing.T) {
	t.Parallel()
	cfg := ffxivlodestone.Config{
		Regions: map[string]ffxivlodestone.RegionConfig{
			"na": {Worlds: []ffxivlodestone.WorldRef{
				{Slug: "gilgamesh", Name: "Gilgamesh"},
				{Slug: "mateus", Name: "Mateus"},
			}},
		},
	}
	dbFake := &fakeDB{}
	h, err := New(Deps{
		ConfigLoader: &fakeConfigLoader{cfg: cfg},
		StatusDB:     dbFake,
		ConfigBucket: "b",
		ConfigKey:    "k",
		StatusSource: &fakeStatusSource{
			source: "frontier",
			byName: map[string]string{"Gilgamesh": "UP", "Mateus": "DOWN"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = h.HandleRequest(context.Background(), events.CloudWatchEvent{ID: "e1"})
	if err != nil {
		t.Fatal(err)
	}
	if dbFake.saves != 2 {
		t.Fatalf("saves: got %d want 2", dbFake.saves)
	}
}

func TestApplyStatuses_missingWorldIncrementsErrors(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{}}
	worlds := []ffxivlodestone.CatalogWorld{{Name: "Missing", Region: "na"}}
	summary := h.applyStatuses(context.Background(), worlds, map[string]string{})
	if summary.Errors != 1 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestApplyStatuses_unchangedCountsSuccess(t *testing.T) {
	t.Parallel()
	h := &Handler{database: &fakeDB{err: db.ErrStatusUnchanged}}
	worlds := []ffxivlodestone.CatalogWorld{{Name: "Gilgamesh", Region: "na"}}
	summary := h.applyStatuses(context.Background(), worlds, map[string]string{"Gilgamesh": "UP"})
	if summary.Successful != 1 || summary.Up != 1 || summary.Errors != 0 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestDefaultStatusSource_usesFrontier(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Gilgamesh":1,"Mateus":0}`))
	}))
	defer srv.Close()

	src := &defaultStatusSource{client: srv.Client()}
	cfg := ffxivlodestone.Config{FrontierStatusURL: srv.URL}

	byName, source, err := src.LoadStatusByWorldName(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if source != "frontier" {
		t.Fatalf("source: %q", source)
	}
	if byName["Gilgamesh"] != "UP" || byName["Mateus"] != "DOWN" {
		t.Fatalf("byName: %+v", byName)
	}
}

func TestDefaultStatusSource_htmlFallbackWhenFrontierFails(t *testing.T) {
	t.Parallel()
	html, err := os.ReadFile(filepath.Join("..", "ffxivlodestone", "testdata", "worldstatus.html"))
	if err != nil {
		t.Fatal(err)
	}

	var frontierHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frontier.json" {
			frontierHits++
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(html)
	}))
	defer srv.Close()

	src := &defaultStatusSource{client: srv.Client()}
	cfg := ffxivlodestone.Config{
		FrontierStatusURL: srv.URL + "/frontier.json",
		LodestoneURL:      srv.URL + "/worldstatus",
	}

	byName, source, err := src.LoadStatusByWorldName(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if frontierHits != 1 {
		t.Fatalf("frontier hits: %d", frontierHits)
	}
	if source != "lodestone" {
		t.Fatalf("source: %q", source)
	}
	if len(byName) < 80 {
		t.Fatalf("expected many worlds from HTML, got %d", len(byName))
	}
}
