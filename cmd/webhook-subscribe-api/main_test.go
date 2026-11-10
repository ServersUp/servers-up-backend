package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/scope"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

type fakeStore struct {
	subs       []models.Subscription
	listCalled int
	addCalled  int
}

func (f *fakeStore) ListSubscriptionsByServer(_ context.Context, serverID string) ([]models.Subscription, error) {
	f.listCalled++
	out := []models.Subscription{}
	for _, s := range f.subs {
		if s.ServerID == serverID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) AddSubscription(_ context.Context, sub models.Subscription) error {
	f.addCalled++
	f.subs = append(f.subs, sub)
	return nil
}

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func testHandler(store *fakeStore) *apiHandler {
	h := &apiHandler{
		db: store,
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       http.NoBody,
			}
		})},
		resolveServer: func(_ context.Context, _, _, _ string) (string, string, error) {
			return "battlenet#us#11", "wow-illidan", nil
		},
	}
	return h
}

func invoke(h *apiHandler, body string, origin string) events.LambdaFunctionURLResponse {
	if body == "" {
		body = `{}`
	}
	headers := map[string]string{}
	if origin != "" {
		headers["origin"] = origin
	}
	req := events.LambdaFunctionURLRequest{
		Body:            body,
		Headers:         headers,
		IsBase64Encoded: false,
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: "POST"},
		},
	}
	resp, _ := h.HandleRequest(context.Background(), req)
	return resp
}

func TestInvalidWebhookURL(t *testing.T) {
	h := testHandler(&fakeStore{})
	body := `{"webhookUrl":"https://evil.example","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 got %d body=%s", resp.StatusCode, resp.Body)
	}
}

func TestHoneypot(t *testing.T) {
	h := testHandler(&fakeStore{})
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","honeypot":"bot trap"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 got %d body=%s", resp.StatusCode, resp.Body)
	}
	if !strings.Contains(resp.Body, `"ok":true`) {
		t.Fatalf("expected ok:true got %s", resp.Body)
	}
}

func TestOriginReject(t *testing.T) {
	h := testHandler(&fakeStore{})
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, "https://evil.example")
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403 got %d", resp.StatusCode)
	}
}

func TestLegacyOriginAllowed(t *testing.T) {
	h := testHandler(&fakeStore{})
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, "https://serversup.armasn.dev")
	if resp.StatusCode == 403 {
		t.Fatalf("legacy origin rejected")
	}
	if got, ok := resp.Headers["Access-Control-Allow-Origin"]; ok {
		t.Fatalf("lambda must not set allow-origin (Function URL CORS does), got %q", got)
	}
}

func TestDuplicate(t *testing.T) {
	store := &fakeStore{subs: []models.Subscription{
		{
			ServerID:   "battlenet#us#11",
			TargetType: "webhook",
			WebhookURL: "https://discord.com/api/webhooks/123/abc",
		},
	}}
	h := testHandler(store)
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409 got %d body=%s", resp.StatusCode, resp.Body)
	}
}

func TestPersistence(t *testing.T) {
	store := &fakeStore{}
	h := testHandler(store)
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","roleId":"123456789012345678","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 got %d body=%s", resp.StatusCode, resp.Body)
	}
	if store.addCalled != 1 {
		t.Fatalf("expected AddSubscription called, got %d", store.addCalled)
	}
	if len(store.subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(store.subs))
	}
	got := store.subs[0]
	if got.TargetType != "webhook" {
		t.Errorf("expected webhook, got %q", got.TargetType)
	}
	if got.Mention != "<@&123456789012345678>" {
		t.Errorf("mention must use <@&id> form, got %q", got.Mention)
	}
	if got.WebhookURL != "https://discord.com/api/webhooks/123/abc" {
		t.Errorf("unexpected webhook url %q", got.WebhookURL)
	}
}

type fakeMappingLoader struct{ m *servermap.Mapping }

func (f *fakeMappingLoader) LoadJSONFromS3(_ context.Context, _, _ string, target any) error {
	*target.(*servermap.Mapping) = *f.m
	return nil
}

type fakeScopeStore struct {
	states   map[string]models.ScopeState
	putCalls int
}

func (f *fakeScopeStore) Get(_ context.Context, key string) (*models.ScopeState, error) {
	st, ok := f.states[key]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (f *fakeScopeStore) Put(_ context.Context, st models.ScopeState) error {
	f.putCalls++
	if f.states == nil {
		f.states = map[string]models.ScopeState{}
	}
	f.states[st.ScopeKey] = st
	return nil
}

type fakeGameStatusLister struct{ rows []models.GameServerStatus }

func (f *fakeGameStatusLister) ListServerStatusesByGame(_ context.Context, _ string) ([]models.GameServerStatus, error) {
	return f.rows, nil
}

func testMapping() *servermap.Mapping {
	return &servermap.Mapping{Games: map[string]servermap.Game{
		"wow": {
			Provider: "battlenet",
			Regions: map[string]servermap.Region{
				"us": {Servers: map[string]servermap.Server{
					"illidan": {Identifier: 57},
				}},
			},
		},
		"forever": {
			Provider: "battlenet",
			Regions: map[string]servermap.Region{
				"us": {Servers: map[string]servermap.Server{
					"pvp":    {Identifier: 1},
					"normal": {Identifier: 2},
				}},
				"kr": {Servers: map[string]servermap.Server{
					"pvp": {Identifier: 1},
				}},
			},
		},
	}}
}

func testScopeHandler(store *fakeStore, scopes *fakeScopeStore) *apiHandler {
	h := testHandler(store)
	h.resolveServer = h.resolveServerViaMapping
	h.configProvider = &fakeMappingLoader{m: testMapping()}
	h.mappingCache = servermap.NewCachedMapping(0)
	h.scopes = scopes
	h.gameStatuses = &fakeGameStatusLister{}
	return h
}

func TestSubscribeRegionScope(t *testing.T) {
	store := &fakeStore{}
	scopes := &fakeScopeStore{}
	h := testScopeHandler(store, scopes)
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow","region":"us"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 got %d body=%s", resp.StatusCode, resp.Body)
	}
	if store.addCalled != 1 {
		t.Fatalf("expected AddSubscription called, got %d", store.addCalled)
	}
	got := store.subs[0]
	if got.ServerID != scope.Key("wow", "us") || got.Scope != scope.TypeRegion || got.Region != "us" || got.GameID != "wow" {
		t.Fatalf("unexpected wildcard subscription: %+v", got)
	}
	if got.ServerLabel != "All WoW servers — US" {
		t.Fatalf("unexpected label: %q", got.ServerLabel)
	}
	if scopes.putCalls != 1 {
		t.Fatalf("expected baseline put, got %d", scopes.putCalls)
	}
	if bl := scopes.states[scope.Key("wow", "us")]; bl.LastNotifiedEpisode == "" {
		t.Fatalf("baseline must be marked notified: %+v", bl)
	}
}

func TestSubscribeMissingRegionRejected(t *testing.T) {
	store := &fakeStore{}
	scopes := &fakeScopeStore{}
	h := testScopeHandler(store, scopes)
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 got %d body=%s", resp.StatusCode, resp.Body)
	}
	if store.addCalled != 0 || scopes.putCalls != 0 {
		t.Fatalf("game-only subscribe must not write: add=%d puts=%d", store.addCalled, scopes.putCalls)
	}
}

func TestSubscribeServerWithoutRegionRejected(t *testing.T) {
	h := testScopeHandler(&fakeStore{}, &fakeScopeStore{})
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","game":"wow","server":"illidan"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 got %d body=%s", resp.StatusCode, resp.Body)
	}
}

func TestResolveTargetScopes(t *testing.T) {
	h := testScopeHandler(&fakeStore{}, &fakeScopeStore{})
	cases := []struct {
		game, region, server, wantPK, wantScope string
		wantErr                                 bool
	}{
		{game: "wow", region: "us", server: "illidan", wantPK: "battlenet#us#57"},
		{game: "wow", region: "us", wantPK: "region#wow#us", wantScope: "region"},
		{game: "wow", wantErr: true},
		{game: "wow", region: "eu", wantErr: true},
		{game: "bogus", wantErr: true},
	}
	for _, tc := range cases {
		pk, _, scopeType, _, _, err := h.resolveTarget(context.Background(), tc.game, tc.region, tc.server)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error for %+v", tc)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %+v: %v", tc, err)
		}
		if pk != tc.wantPK || scopeType != tc.wantScope {
			t.Fatalf("resolveTarget(%q,%q,%q) = (%q, scope %q), want (%q, %q)", tc.game, tc.region, tc.server, pk, scopeType, tc.wantPK, tc.wantScope)
		}
	}
}

func TestResolveTargetForever(t *testing.T) {
	h := testScopeHandler(&fakeStore{}, &fakeScopeStore{})
	pk, label, scopeType, game, _, err := h.resolveTarget(context.Background(), "forever", "us", "normal")
	if err != nil || pk != "battlenet#us#2" || label != "wow-forever-us-normal" || scopeType != "" || game != "forever" {
		t.Fatalf("got %q %q %q %q %v", pk, label, scopeType, game, err)
	}
	pk, label, scopeType, _, _, err = h.resolveTarget(context.Background(), "forever", "us", "")
	if err != nil || pk != "region#forever#us" || scopeType != "region" || label != "All WoW Forever servers — US" {
		t.Fatalf("got %q %q %q %v", pk, label, scopeType, err)
	}
	if _, _, _, _, _, err := h.resolveTarget(context.Background(), "forever", "kr", "roleplay"); err == nil {
		t.Fatal("KR has no roleplay ruleset")
	}
	if _, _, _, _, _, err := h.resolveTarget(context.Background(), "forever", "eu", ""); err == nil {
		t.Fatal("EU is not in this fixture")
	}
}

func TestResolveTargetWowForeverAlias(t *testing.T) {
	h := testScopeHandler(&fakeStore{}, &fakeScopeStore{})
	for _, game := range []string{"wow-forever", "WoW Forever", "forever"} {
		pk, label, scopeType, gameID, _, err := h.resolveTarget(context.Background(), game, "us", "normal")
		if err != nil || pk != "battlenet#us#2" || label != "wow-forever-us-normal" || scopeType != "" || gameID != "forever" {
			t.Fatalf("%q: got %q %q %q %q %v", game, pk, label, scopeType, gameID, err)
		}
	}
	pk, label, scopeType, gameID, _, err := h.resolveTarget(context.Background(), "wow-forever", "us", "")
	if err != nil || pk != "region#forever#us" || scopeType != "region" || gameID != "forever" || label != "All WoW Forever servers — US" {
		t.Fatalf("region: got %q %q %q %q %v", pk, label, scopeType, gameID, err)
	}
}

func TestProveWebhookForeverNote(t *testing.T) {
	var sent []string
	h := &apiHandler{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		b, _ := io.ReadAll(req.Body)
		var payload struct{ Content string }
		_ = json.Unmarshal(b, &payload) // decode: the encoder escapes <, @ and & as \u00XX
		sent = append(sent, payload.Content)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: http.NoBody}
	})}}
	const note = "may miss short ones while the game is in early launch"
	for _, tc := range []struct {
		game     string
		wantNote bool
	}{{"forever", true}, {"wow", false}, {"ffxiv", false}} {
		sent = nil
		if err := h.proveWebhook(context.Background(), "https://discord.com/api/webhooks/1/abc", "", tc.game); err != nil {
			t.Fatal(err)
		}
		if len(sent) != 1 || !strings.Contains(sent[0], "ServersUp ownership verification") {
			t.Fatalf("%s: %v", tc.game, sent)
		}
		if got := strings.Contains(sent[0], note); got != tc.wantNote {
			t.Fatalf("%s: note present=%v want %v in %s", tc.game, got, tc.wantNote, sent[0])
		}
	}
	// the role mention still leads the message
	sent = nil
	_ = h.proveWebhook(context.Background(), "https://discord.com/api/webhooks/1/abc", "123", "forever")
	if !strings.Contains(sent[0], "<@&123> ServersUp ownership verification") {
		t.Fatalf("role mention lost: %s", sent[0])
	}
}

func TestInvalidRoleID(t *testing.T) {
	store := &fakeStore{}
	h := testHandler(store)
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","roleId":"1> @everyone <@&1","game":"wow","region":"us","server":"illidan"}`
	resp := invoke(h, body, allowedOrigin)
	if resp.StatusCode != 400 || store.addCalled != 0 {
		t.Fatalf("expected 400 and no save, got %d adds=%d", resp.StatusCode, store.addCalled)
	}
}
