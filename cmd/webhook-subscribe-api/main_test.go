package main

import (
	"context"
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
	body := `{"webhookUrl":"https://discord.com/api/webhooks/123/abc","roleId":"456","game":"wow","region":"us","server":"illidan"}`
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
