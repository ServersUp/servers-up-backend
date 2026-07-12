package statussnapshot

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeConfigLoader struct {
	mapping servermap.Mapping
	err     error
}

func (f *fakeConfigLoader) LoadJSONFromS3(_ context.Context, _, _ string, target any) error {
	if f.err != nil {
		return f.err
	}
	b, err := json.Marshal(f.mapping)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

type fakeStatusDB struct {
	byGame map[string][]models.GameServerStatus
	err    error
}

func (f *fakeStatusDB) ListServerStatusesByGame(_ context.Context, gameID string) ([]models.GameServerStatus, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byGame[gameID], nil
}

type fakeS3 struct {
	putIn *s3.PutObjectInput
	err   error
}

func (f *fakeS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putIn = params
	if f.err != nil {
		return nil, f.err
	}
	return &s3.PutObjectOutput{}, nil
}

func testMapping() servermap.Mapping {
	return servermap.Mapping{
		Games: map[string]servermap.Game{
			"wow": {
				Provider: "battlenet",
				Regions: map[string]servermap.Region{
					"us": {
						Servers: map[string]servermap.Server{
							"illidan": {Identifier: 57},
							"thrall":  {Identifier: 1071},
						},
					},
				},
			},
		},
	}
}

func TestBuildSnapshot_joinsStatusAndUnknown(t *testing.T) {
	t.Parallel()

	db := &fakeStatusDB{
		byGame: map[string][]models.GameServerStatus{
			"wow": {{
				GameID:        "wow",
				ServerID:      "battlenet#us#57",
				Status:        "UP",
				LastUpdatedAt: 1710000000,
			}},
		},
	}

	snap, err := BuildSnapshot(context.Background(), testMapping(), db, 1710000099)
	if err != nil {
		t.Fatal(err)
	}
	if snap.GeneratedAt != 1710000099 {
		t.Fatalf("generatedAt: %d", snap.GeneratedAt)
	}
	servers := snap.Games["wow"].Regions["us"].Servers
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %#v", servers)
	}
	bySlug := map[string]ServerSnap{}
	for _, s := range servers {
		bySlug[s.Slug] = s
	}
	if bySlug["illidan"].Status != "UP" || bySlug["illidan"].Label != "wow-us-illidan" || bySlug["illidan"].LastUpdatedAt != 1710000000 {
		t.Fatalf("illidan: %#v", bySlug["illidan"])
	}
	if bySlug["thrall"].Status != StatusUnknown || bySlug["thrall"].LastUpdatedAt != 0 {
		t.Fatalf("thrall: %#v", bySlug["thrall"])
	}
}

func TestHandleRequest_putsJSON(t *testing.T) {
	t.Parallel()

	s3fake := &fakeS3{}
	h, err := New(Deps{
		ConfigLoader:     &fakeConfigLoader{mapping: testMapping()},
		StatusDB:         &fakeStatusDB{byGame: map[string][]models.GameServerStatus{}},
		S3:               s3fake,
		ConfigBucket:     "serversup-config",
		ServerMappingKey: "server-mapping.json",
		SnapshotBucket:   "serversup-status-public",
		SnapshotKey:      "status/latest.json",
		Now:              func() time.Time { return time.Unix(1710000099, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.HandleRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s3fake.putIn == nil {
		t.Fatal("expected PutObject")
	}
	if *s3fake.putIn.Bucket != "serversup-status-public" || *s3fake.putIn.Key != "status/latest.json" {
		t.Fatalf("unexpected put target: %s/%s", *s3fake.putIn.Bucket, *s3fake.putIn.Key)
	}
	if *s3fake.putIn.CacheControl != cacheControlPublic {
		t.Fatalf("cache-control: %q", *s3fake.putIn.CacheControl)
	}
	body, err := io.ReadAll(s3fake.putIn.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal put body: %v (%s)", err, body)
	}
	if got.GeneratedAt != 1710000099 {
		t.Fatalf("unmarshaled generatedAt: %d", got.GeneratedAt)
	}
}

func TestNew_requiresDeps(t *testing.T) {
	t.Parallel()
	if _, err := New(Deps{}); err == nil {
		t.Fatal("expected error")
	}
}
