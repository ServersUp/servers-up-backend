package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
)

type fakeStore struct {
	subs            []models.Subscription
	listCalled      int
	addCalled       int
}

func (f *fakeStore) ListSubscriptions(_ context.Context, serverID string) ([]models.Subscription, error) {
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
		db:         store,
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
		Body:       body,
		Headers:    headers,
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
