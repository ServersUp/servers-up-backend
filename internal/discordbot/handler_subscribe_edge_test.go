package discordbot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
)

// edgeMapping is a catalog with multiple regions, a hyphenated server, a
// region with no servers and a second provider.
func edgeMapping(m *servermap.Mapping) {
	m.Games = map[string]servermap.Game{
		"wow": {Provider: "battlenet", Regions: map[string]servermap.Region{
			"us": {Servers: map[string]servermap.Server{"illidan": {Identifier: 57}, "area-52": {Identifier: 3676}}},
			"eu": {Servers: map[string]servermap.Server{"draenor": {Identifier: 1}}},
		}},
		"ffxiv": {Provider: "lodestone", Regions: map[string]servermap.Region{
			"na": {Servers: map[string]servermap.Server{"alpha": {Identifier: 1}}},
		}},
		"ghost": {Provider: "other", Regions: map[string]servermap.Region{
			"void": {Servers: map[string]servermap.Server{}},
		}},
	}
}

type edgeFixture struct {
	*testHandlerFixture
	added   []models.Subscription
	deleted []string
}

func newEdgeFixture(t *testing.T) *edgeFixture {
	t.Helper()
	f := &edgeFixture{testHandlerFixture: newTestHandlerFixture(t)}
	f.config.LoadFunc = func(_ context.Context, _, _ string, target any) error {
		edgeMapping(target.(*servermap.Mapping))
		return nil
	}
	f.db.ListFunc = func(context.Context, string) ([]models.Subscription, error) { return nil, nil }
	f.db.AddFunc = func(_ context.Context, s models.Subscription) error {
		f.added = append(f.added, s)
		return nil
	}
	f.db.DeleteFunc = func(_ context.Context, _, _, _, subID string) error {
		f.deleted = append(f.deleted, subID)
		return nil
	}
	return f
}

// body builds a subscribe interaction. opts are name,value pairs; a value of
// the form "#123" is emitted as a JSON number to exercise wrong-typed options.
func edgeBody(guild, channel, perms string, opts ...string) string {
	var os []string
	for i := 0; i+1 < len(opts); i += 2 {
		v := fmt.Sprintf("%q", opts[i+1])
		if strings.HasPrefix(opts[i+1], "#") {
			v = opts[i+1][1:]
		}
		os = append(os, fmt.Sprintf(`{"name": %q, "value": %s}`, opts[i], v))
	}
	member := `"member": {"user": {"id": "user-1"}, "permissions": "` + perms + `"},`
	if perms == "<nomember>" {
		member = ""
	}
	return `{"type": 2, "guild_id": "` + guild + `", "channel_id": "` + channel + `", ` + member +
		` "data": {"name": "subscribe", "options": [` + strings.Join(os, ",") + `]}}`
}

func TestSubscribeEdge_Inputs(t *testing.T) {
	t.Parallel()
	const admin = "8"
	cases := []struct {
		name      string
		body      string
		wantPK    string // expected subscription pk; empty means no row may be written
		wantInMsg string
	}{
		// permission gate
		{"no member in guild", edgeBody("g1", "c1", "<nomember>", "game", "wow", "region", "us", "server", "illidan"), "", "Manage Channels"},
		{"empty permissions", edgeBody("g1", "c1", "", "game", "wow", "region", "us", "server", "illidan"), "", "Manage Channels"},
		{"garbage permissions", edgeBody("g1", "c1", "abc", "game", "wow", "region", "us", "server", "illidan"), "", "Manage Channels"},
		{"zero permissions", edgeBody("g1", "c1", "0", "game", "wow", "region", "us", "server", "illidan"), "", "Manage Channels"},
		{"manage channels only", edgeBody("g1", "c1", "16", "game", "wow", "region", "us", "server", "illidan"), "battlenet#us#57", "Subscribed"},
		{"administrator only", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "illidan"), "battlenet#us#57", "Subscribed"},
		{"DM has no guild", edgeBody("", "c1", "<nomember>", "game", "wow", "region", "us", "server", "illidan"), "", "server"},
		{"missing channel id", edgeBody("g1", "", admin, "game", "wow", "region", "us", "server", "illidan"), "", "channel"},

		// normalization
		{"mixed case and padding", edgeBody("g1", "c1", admin, "game", "  WoW ", "region", "US", "server", " ILLIDAN "), "battlenet#us#57", "Subscribed"},
		{"hyphenated server typed with space", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "Area 52"), "battlenet#us#3676", "Subscribed"},
		{"hyphenated server typed with underscore", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "area_52"), "battlenet#us#3676", "Subscribed"},
		{"other provider", edgeBody("g1", "c1", admin, "game", "ffxiv", "region", "na", "server", "alpha"), "lodestone#na#1", "Subscribed"},

		// missing / blank inputs
		{"no options", edgeBody("g1", "c1", admin), "", "game"},
		{"blank game", edgeBody("g1", "c1", admin, "game", "   ", "region", "us"), "", "game"},
		{"server without region", edgeBody("g1", "c1", admin, "game", "wow", "server", "illidan"), "", "region"},
		{"game only", edgeBody("g1", "c1", admin, "game", "wow"), "", "region"},
		{"region only", edgeBody("g1", "c1", admin, "region", "us"), "", "game"},
		{"numeric game", edgeBody("g1", "c1", admin, "game", "#5", "region", "us"), "", "game"},
		{"numeric region", edgeBody("g1", "c1", admin, "game", "wow", "region", "#5"), "", "region"},
		{"numeric server falls back to wildcard? must not", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "#5"), "region#wow#us", "Subscribed"},

		// wrong / hostile values
		{"unknown game", edgeBody("g1", "c1", admin, "game", "nope", "region", "us"), "", "Unknown game"},
		{"unknown region", edgeBody("g1", "c1", admin, "game", "wow", "region", "kr"), "", "Unknown region"},
		{"region of another game", edgeBody("g1", "c1", admin, "game", "wow", "region", "na"), "", "Unknown region"},
		{"unknown server", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "nope"), "", ""},
		{"server of other region", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "draenor"), "", ""},
		{"scope key as game", edgeBody("g1", "c1", admin, "game", "region#wow", "region", "us"), "", "Unknown game"},
		{"hash in server", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "illidan#1"), "", ""},
		{"very long game", edgeBody("g1", "c1", admin, "game", strings.Repeat("a", 20000), "region", "us"), "", "Unknown game"},
		{"unicode server", edgeBody("g1", "c1", admin, "game", "wow", "region", "us", "server", "иллидан"), "", ""},
		{"only separators", edgeBody("g1", "c1", admin, "game", "---", "region", "us"), "", "game"},

		// wildcard
		{"region wildcard", edgeBody("g1", "c1", admin, "game", "wow", "region", "us"), "region#wow#us", "all"},
		{"region wildcard other game", edgeBody("g1", "c1", admin, "game", "ffxiv", "region", "na"), "region#ffxiv#na", "all"},
		{"wildcard of empty region", edgeBody("g1", "c1", admin, "game", "ghost", "region", "void"), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEdgeFixture(t)
			content := subscribeRespContent(t, f.testHandlerFixture, tc.body)
			if tc.wantPK == "" {
				if len(f.added) != 0 {
					t.Fatalf("no row expected, wrote %+v (reply %q)", f.added, content)
				}
			} else {
				if len(f.added) != 1 || f.added[0].ServerID != tc.wantPK {
					t.Fatalf("want one row %q, got %+v (reply %q)", tc.wantPK, f.added, content)
				}
				if f.added[0].GuildID == "" || f.added[0].ChannelID == "" {
					t.Fatalf("row missing guild/channel: %+v", f.added[0])
				}
			}
			if !strings.Contains(strings.ToLower(content), strings.ToLower(tc.wantInMsg)) {
				t.Fatalf("reply %q should contain %q", content, tc.wantInMsg)
			}
		})
	}
}

func TestSubscribeEdge_Roles(t *testing.T) {
	t.Parallel()
	f := newEdgeFixture(t)
	subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us", "server", "illidan", "role", "777"))
	if len(f.added) != 1 || f.added[0].Mention != "<@&777>" {
		t.Fatalf("role mention not stored: %+v", f.added)
	}
	f = newEdgeFixture(t)
	subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us", "server", "illidan", "role", "#777"))
	if len(f.added) != 1 || f.added[0].Mention != "" {
		t.Fatalf("non-string role must be ignored: %+v", f.added)
	}
}

func TestSubscribeEdge_Duplicates(t *testing.T) {
	t.Parallel()
	existing := func(channel, pk string) []models.Subscription {
		return []models.Subscription{{ServerID: pk, ChannelID: channel, GuildID: "g1"}}
	}
	cases := []struct {
		name    string
		have    []models.Subscription
		opts    []string
		wantAdd int
	}{
		{"same server same channel", existing("c1", "battlenet#us#57"), []string{"game", "wow", "region", "us", "server", "illidan"}, 0},
		{"same server other channel", existing("c2", "battlenet#us#57"), []string{"game", "wow", "region", "us", "server", "illidan"}, 1},
		{"other server same channel", existing("c1", "battlenet#us#3676"), []string{"game", "wow", "region", "us", "server", "illidan"}, 1},
		{"wildcard same channel", existing("c1", "region#wow#us"), []string{"game", "wow", "region", "us"}, 0},
		{"wildcard other region", existing("c1", "region#wow#eu"), []string{"game", "wow", "region", "us"}, 1},
		{"server under existing wildcard", existing("c1", "region#wow#us"), []string{"game", "wow", "region", "us", "server", "illidan"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEdgeFixture(t)
			f.db.ListFunc = func(context.Context, string) ([]models.Subscription, error) { return tc.have, nil }
			content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", tc.opts...))
			if len(f.added) != tc.wantAdd {
				t.Fatalf("want %d rows, got %d (reply %q)", tc.wantAdd, len(f.added), content)
			}
		})
	}
}

// A repeated wildcard subscribe must repair a missing ScopeState row, which is
// the state left behind by the earlier missing-env-var panics.
func TestSubscribeEdge_DuplicateWildcardHealsMissingScopeState(t *testing.T) {
	t.Parallel()
	f := newEdgeFixture(t)
	f.db.ListFunc = func(context.Context, string) ([]models.Subscription, error) {
		return []models.Subscription{{ServerID: "region#wow#us", ChannelID: "c1", GuildID: "g1"}}, nil
	}
	content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us"))
	if len(f.added) != 0 {
		t.Fatal("duplicate must not add a row")
	}
	if len(f.scopes.PutCalls) != 1 || f.scopes.PutCalls[0].ScopeKey != scope.Key("wow", "us") {
		t.Fatalf("missing ScopeState should be baselined, puts=%+v (reply %q)", f.scopes.PutCalls, content)
	}
}

func TestSubscribeEdge_DuplicateWildcardKeepsExistingScopeState(t *testing.T) {
	t.Parallel()
	f := newEdgeFixture(t)
	f.scopes.states = map[string]models.ScopeState{scope.Key("wow", "us"): {ScopeKey: scope.Key("wow", "us"), State: scope.StateAllDown}}
	f.db.ListFunc = func(context.Context, string) ([]models.Subscription, error) {
		return []models.Subscription{{ServerID: "region#wow#us", ChannelID: "c1", GuildID: "g1"}}, nil
	}
	subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us"))
	if len(f.scopes.PutCalls) != 0 {
		t.Fatalf("existing state must not be overwritten: %+v", f.scopes.PutCalls)
	}
}

func TestSubscribeEdge_Baseline(t *testing.T) {
	t.Parallel()
	statuses := func(a, b string) *MockGameStatusLister {
		return &MockGameStatusLister{ByGame: map[string][]models.GameServerStatus{"wow": {
			{ServerID: "battlenet#us#57", Status: a},
			{ServerID: "battlenet#us#3676", Status: b},
			{ServerID: "battlenet#eu#1", Status: "DOWN"}, // other region must not count
		}}}
	}
	cases := []struct {
		name            string
		lister          *MockGameStatusLister
		wantUp, wantTot int
		wantState       string
	}{
		{"mixed", statuses("UP", "DOWN"), 1, 2, scope.StateMixed},
		{"all up", statuses("UP", "UP"), 2, 2, scope.StateAllUp},
		{"all down", statuses("DOWN", "DOWN"), 0, 2, scope.StateAllDown},
		{"no status rows counts as down", &MockGameStatusLister{}, 0, 2, scope.StateAllDown},
		{"degraded is not up", statuses("DEGRADED", "UP"), 1, 2, scope.StateMixed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEdgeFixture(t)
			f.handler.gameStatuses = tc.lister
			subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us"))
			if len(f.scopes.PutCalls) != 1 {
				t.Fatalf("want 1 baseline, got %d", len(f.scopes.PutCalls))
			}
			st := f.scopes.PutCalls[0]
			if st.UpCount != tc.wantUp || st.TotalCount != tc.wantTot || st.State != tc.wantState {
				t.Fatalf("baseline %+v, want up=%d total=%d state=%s", st, tc.wantUp, tc.wantTot, tc.wantState)
			}
		})
	}
}

func TestSubscribeEdge_ServerSubscribeNeedsNoScopeStore(t *testing.T) {
	t.Parallel()
	f := newEdgeFixture(t)
	f.handler.scopeStates = nil
	content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", "game", "wow", "region", "us", "server", "illidan"))
	if len(f.added) != 1 || !strings.Contains(content, "Subscribed") {
		t.Fatalf("single-server subscribe must work without the scope store: %+v %q", f.added, content)
	}
}

// failingScopes lets individual scope store calls fail.
type failingScopes struct {
	MockScopeStateStore
	getErr, putErr error
}

func (s *failingScopes) Get(ctx context.Context, k string) (*models.ScopeState, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.MockScopeStateStore.Get(ctx, k)
}

func (s *failingScopes) Put(ctx context.Context, st models.ScopeState) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.MockScopeStateStore.Put(ctx, st)
}

func TestSubscribeEdge_DependencyFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	opts := []string{"game", "wow", "region", "us", "server", "illidan"}
	wild := []string{"game", "wow", "region", "us"}

	t.Run("mapping load fails", func(t *testing.T) {
		t.Parallel()
		f := newEdgeFixture(t)
		f.config.LoadFunc = func(context.Context, string, string, any) error { return boom }
		content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", opts...))
		if len(f.added) != 0 || !strings.Contains(content, "System error") {
			t.Fatalf("%+v %q", f.added, content)
		}
	})
	t.Run("list fails", func(t *testing.T) {
		t.Parallel()
		f := newEdgeFixture(t)
		f.db.ListFunc = func(context.Context, string) ([]models.Subscription, error) { return nil, boom }
		content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", opts...))
		if len(f.added) != 0 || !strings.Contains(content, "Failed") {
			t.Fatalf("%+v %q", f.added, content)
		}
	})
	t.Run("add fails", func(t *testing.T) {
		t.Parallel()
		f := newEdgeFixture(t)
		f.db.AddFunc = func(context.Context, models.Subscription) error { return boom }
		content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", wild...))
		if !strings.Contains(content, "Failed") || len(f.scopes.PutCalls) != 0 {
			t.Fatalf("%q puts=%d", content, len(f.scopes.PutCalls))
		}
	})
	// A wildcard row without a ScopeState row is invisible to the aggregator
	// (it only iterates ScopeState), so a baseline failure must not leave the
	// subscription behind while telling the user it worked.
	for _, tc := range []struct {
		name string
		st   *failingScopes
	}{
		{"scope get fails", &failingScopes{getErr: boom}},
		{"scope put fails", &failingScopes{putErr: boom}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEdgeFixture(t)
			f.handler.scopeStates = tc.st
			content := subscribeRespContent(t, f.testHandlerFixture, edgeBody("g1", "c1", "8", wild...))
			if len(f.added) != 1 {
				t.Fatalf("expected the add attempt, got %d", len(f.added))
			}
			if len(f.deleted) != 1 || f.deleted[0] != f.added[0].SubscriptionID {
				t.Fatalf("orphan wildcard row must be rolled back, deleted=%v", f.deleted)
			}
			if strings.Contains(content, "Subscribed") {
				t.Fatalf("must not claim success: %q", content)
			}
		})
	}
}
