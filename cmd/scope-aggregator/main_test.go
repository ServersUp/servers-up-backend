package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/aggregate"
	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/serverid"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func testMapping() servermap.Mapping {
	return servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {
			Provider: "battlenet",
			Regions: map[string]servermap.Region{
				"us": {Servers: map[string]servermap.Server{
					"illidan":     {Identifier: 57},
					"stormrage":   {Identifier: 5},
					"tichondrius": {Identifier: 60},
				}},
			},
		},
	}}
}

func testStatuses(up ...string) map[string]string {
	out := map[string]string{
		serverid.Generate("battlenet", "us", 57): "DOWN",
		serverid.Generate("battlenet", "us", 5):  "DOWN",
		serverid.Generate("battlenet", "us", 60): "DOWN",
	}
	for _, id := range up {
		out[serverid.Generate("battlenet", "us", id)] = "UP"
	}
	return out
}

type fakeScopeStore struct {
	states   map[string]models.ScopeState
	puts     []models.ScopeState
	claims   []string
	claimOK  bool
	claimErr error
}

func (f *fakeScopeStore) List(_ context.Context) ([]models.ScopeState, error) {
	var out []models.ScopeState
	for _, st := range f.states {
		out = append(out, st)
	}
	return out, nil
}

func (f *fakeScopeStore) Get(_ context.Context, key string) (*models.ScopeState, error) {
	st, ok := f.states[key]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (f *fakeScopeStore) Put(_ context.Context, st models.ScopeState) error {
	if f.states == nil {
		f.states = map[string]models.ScopeState{}
	}
	f.puts = append(f.puts, st)
	f.states[st.ScopeKey] = st
	return nil
}

func (f *fakeScopeStore) Delete(_ context.Context, _ string) error { return nil }

func (f *fakeScopeStore) ClaimNotify(_ context.Context, _ models.ScopeState, ep string) (bool, error) {
	f.claims = append(f.claims, ep)
	return f.claimOK, f.claimErr
}

type fakeSubLister struct {
	byServer map[string][]models.Subscription
}

func (f *fakeSubLister) ListSubscriptionsByServer(_ context.Context, serverID string) ([]models.Subscription, error) {
	return f.byServer[serverID], nil
}

type fakeStatusLister struct {
	byGame map[string][]models.GameServerStatus
}

func (f *fakeStatusLister) ListServerStatusesByGame(_ context.Context, gameID string) ([]models.GameServerStatus, error) {
	return f.byGame[gameID], nil
}

func statusRowsByGame(statusByServer map[string]string) map[string][]models.GameServerStatus {
	byGame := map[string][]models.GameServerStatus{}
	for serverID, status := range statusByServer {
		byGame["wow"] = append(byGame["wow"], models.GameServerStatus{GameID: "wow", ServerID: serverID, Status: status})
	}
	return byGame
}

type fakeSender struct {
	sent []models.GuildNotifyJob
	err  error
}

func (f *fakeSender) SendMessage(_ context.Context, params *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	var job models.GuildNotifyJob
	if err := json.Unmarshal([]byte(*params.MessageBody), &job); err != nil {
		return nil, err
	}
	f.sent = append(f.sent, job)
	return &sqs.SendMessageOutput{}, f.err
}

func testHandler(fs *fakeScopeStore, subs *fakeSubLister, statuses *fakeStatusLister, sender *fakeSender, settle time.Duration) *Handler {
	if settle == 0 {
		settle = 5 * time.Minute
	}
	m := testMapping()
	return &Handler{
		scopes:       fs,
		subs:         subs,
		statuses:     statuses,
		queueURL:     "https://sqs.test/queue",
		sqs:          sender,
		settleWindow: settle,
		mapping:      &m,
	}
}

func TestProcessScope_baselineInitializesWithoutNotify(t *testing.T) {
	fs := &fakeScopeStore{}
	h := testHandler(fs, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 0)
	cur := models.ScopeState{ScopeKey: scope.Key("wow", "")}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses(), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.puts) != 1 {
		t.Fatalf("expected 1 baseline put, got %d", len(fs.puts))
	}
	got := fs.puts[0]
	if got.State != scope.StateAllDown {
		t.Fatalf("expected ALL_DOWN baseline (no servers up), got %s", got.State)
	}
	if got.LastNotifiedEpisode == "" {
		t.Fatal("baseline must set lastNotifiedEpisode so first observation never notifies")
	}
	if len(fs.claims) != 0 || len(pending) != 0 {
		t.Fatal("baseline must not claim or enqueue")
	}
}

func TestProcessScope_stateChangeToMixedNoNotify(t *testing.T) {
	fs := &fakeScopeStore{}
	h := testHandler(fs, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 0)
	cur := models.ScopeState{
		ScopeKey:            scope.Key("wow", ""),
		TotalCount:          3,
		UpCount:             3,
		State:               scope.StateAllUp,
		StateSince:          50,
		LastNotifiedEpisode: aggregate.Episode(scope.StateAllUp, 50),
	}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses("57", "5"), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(fs.puts))
	}
	got := fs.puts[0]
	if got.State != scope.StateMixed {
		t.Fatalf("expected MIXED, got %s", got.State)
	}
	if got.StateSince != 100 {
		t.Fatalf("expected stateSince reset to 100, got %d", got.StateSince)
	}
	if len(fs.claims) != 0 || len(pending) != 0 {
		t.Fatal("MIXED must not notify")
	}
}

func TestProcessScope_terminalSettleNotElapsed(t *testing.T) {
	fs := &fakeScopeStore{claimOK: true}
	h := testHandler(fs, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 5*time.Minute)
	cur := models.ScopeState{
		ScopeKey:            scope.Key("wow", ""),
		TotalCount:          3,
		UpCount:             0,
		State:               scope.StateAllDown,
		StateSince:          100 - 60, // 1 minute ago, settle is 5
		LastNotifiedEpisode: "",
	}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses(), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.claims) != 0 || len(pending) != 0 || len(fs.puts) != 0 {
		t.Fatal("must not claim/notify/put before settle elapses")
	}
}

func TestProcessScope_terminalSettleElapsedClaimsAndCollects(t *testing.T) {
	fs := &fakeScopeStore{claimOK: true}
	subs := &fakeSubLister{byServer: map[string][]models.Subscription{
		scope.Key("wow", ""): {
			{GuildID: "g1", ChannelID: "c1", Mention: "<@&7>", TargetType: "bot"},
		},
	}}
	h := testHandler(fs, subs, &fakeStatusLister{}, &fakeSender{}, 5*time.Minute)
	cur := models.ScopeState{
		ScopeKey:   scope.Key("wow", ""),
		TotalCount: 3,
		UpCount:    0,
		State:      scope.StateAllDown,
		StateSince: 100 - 400, // > settle
	}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses(), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(fs.claims))
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending delivery, got %d", len(pending))
	}
	var d *delivery
	for _, v := range pending {
		d = v
	}
	if !d.job.Aggregate || d.job.ScopeLabel != "WoW" || d.job.Status != "DOWN" || d.job.RoleID != "7" {
		t.Fatalf("unexpected aggregate job: %+v", d.job)
	}
}

func TestProcessScope_alreadyNotifiedSkips(t *testing.T) {
	fs := &fakeScopeStore{claimOK: true}
	h := testHandler(fs, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 5*time.Minute)
	ep := aggregate.Episode(scope.StateAllDown, 50)
	cur := models.ScopeState{
		ScopeKey:            scope.Key("wow", ""),
		TotalCount:          3,
		UpCount:             0,
		State:               scope.StateAllDown,
		StateSince:          50,
		LastNotifiedEpisode: ep,
	}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses(), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.claims) != 0 || len(pending) != 0 {
		t.Fatal("already-notified episode must not claim again")
	}
}

func TestProcessScope_claimLostSkips(t *testing.T) {
	fs := &fakeScopeStore{claimOK: false}
	h := testHandler(fs, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 5*time.Minute)
	cur := models.ScopeState{
		ScopeKey:   scope.Key("wow", ""),
		TotalCount: 3,
		UpCount:    0,
		State:      scope.StateAllDown,
		StateSince: -301,
	}
	pending := map[string]*delivery{}

	if err := h.processScope(context.Background(), testMapping(), testStatuses(), &cur, 100, pending); err != nil {
		t.Fatal(err)
	}
	if len(fs.claims) != 1 || len(pending) != 0 {
		t.Fatal("lost claim must not enqueue")
	}
}

func TestHandleRequest_endToEndEnqueuesAggregateJob(t *testing.T) {
	// scope ALL_DOWN past settle with 1 bot subscriber
	now := time.Now().Unix()
	fs := &fakeScopeStore{claimOK: true, states: map[string]models.ScopeState{
		scope.Key("wow", "us"): {
			ScopeKey:   scope.Key("wow", "us"),
			TotalCount: 3,
			UpCount:    0,
			State:      scope.StateAllDown,
			StateSince: now - 400,
		},
	}}
	subs := &fakeSubLister{byServer: map[string][]models.Subscription{
		scope.Key("wow", "us"): {
			{GuildID: "g1", ChannelID: "c1", Mention: "<@&9>", TargetType: "bot"},
		},
	}}
	sender := &fakeSender{}
	h := testHandler(fs, subs, &fakeStatusLister{byGame: statusRowsByGame(testStatuses())}, sender, 5*time.Minute)

	if err := h.HandleRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 SQS job, got %d", len(sender.sent))
	}
	job := sender.sent[0]
	if !job.Aggregate || job.ScopeLabel != "WoW US" || job.Status != "DOWN" {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.GuildID != "g1" || job.ChannelID != "c1" || job.RoleID != "9" {
		t.Fatalf("unexpected delivery fields: %+v", job)
	}
}

func TestHandleRequest_overlapDedupeRegionWins(t *testing.T) {
	now := time.Now().Unix()
	regionKey := scope.Key("wow", "us")
	gameKey := scope.Key("wow", "")
	fs := &fakeScopeStore{claimOK: true, states: map[string]models.ScopeState{
		regionKey: {
			ScopeKey:   regionKey,
			TotalCount: 3,
			UpCount:    0,
			State:      scope.StateAllDown,
			StateSince: now - 400,
		},
		gameKey: {
			ScopeKey:   gameKey,
			TotalCount: 3,
			UpCount:    0,
			State:      scope.StateAllDown,
			StateSince: now - 400,
		},
	}}
	// same channel subscribed to both scopes
	subs := &fakeSubLister{byServer: map[string][]models.Subscription{
		regionKey: {{GuildID: "g1", ChannelID: "c1", TargetType: "bot"}},
		gameKey:   {{GuildID: "g1", ChannelID: "c1", TargetType: "bot"}},
	}}
	sender := &fakeSender{}
	h := testHandler(fs, subs, &fakeStatusLister{byGame: statusRowsByGame(testStatuses())}, sender, 5*time.Minute)

	if err := h.HandleRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 deduped SQS job, got %d", len(sender.sent))
	}
	if sender.sent[0].Scope != scope.TypeRegion {
		t.Fatalf("expected region scope to win, got %s", sender.sent[0].Scope)
	}
}

func TestHandleRequest_noActiveScopesIdle(t *testing.T) {
	h := testHandler(&fakeScopeStore{}, &fakeSubLister{}, &fakeStatusLister{}, &fakeSender{}, 0)
	if err := h.HandleRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestScopeLabelFor(t *testing.T) {
	if got := scopeLabelFor(scope.Key("wow", "")); got != "WoW" {
		t.Fatalf("game scope label = %q", got)
	}
	if got := scopeLabelFor(scope.Key("wow", "eu")); got != "WoW EU" {
		t.Fatalf("region scope label = %q", got)
	}
	if got := scopeLabelFor(scope.Key("ffxiv", "na")); got != "FFXIV NA" {
		t.Fatalf("ffxiv region label = %q", got)
	}
}

func TestResolveRoleID(t *testing.T) {
	cases := map[string]string{
		"<@&12345>": "12345",
		"12345":     "12345",
		"":          "",
		"<@some>":   "",
	}
	for in, want := range cases {
		if got := resolveRoleID(in); got != want {
			t.Fatalf("resolveRoleID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeliveryKey(t *testing.T) {
	if got := deliveryKey(&models.Subscription{GuildID: "g", ChannelID: "c", TargetType: "bot"}); got != "bot:g:c" {
		t.Fatalf("bot key = %q", got)
	}
	if got := deliveryKey(&models.Subscription{TargetType: "webhook", WebhookURL: "HTTPS://X"}); got != "webhook:https://x" {
		t.Fatalf("webhook key = %q", got)
	}
}
