package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// redirectTransport rewrites all request URLs to target the given base URL
// while preserving path and query. This lets tests use a real http.Client
// against an httptest.Server without touching the package-level apiBase var.
type redirectTransport struct {
	base  string
	inner http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	base, _ := url.Parse(t.base)
	r2.URL.Scheme = base.Scheme
	r2.URL.Host = base.Host
	return t.inner.RoundTrip(r2)
}

// testClientForServer returns an http.Client that sends all requests to srv.
func testClientForServer(srv *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{
			base:  srv.URL,
			inner: srv.Client().Transport,
		},
	}
}

func TestGuildRoleName_success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot mytoken" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		roles := []apiRole{{ID: "role-1", Name: "Admins"}, {ID: "role-2", Name: "Mods"}}
		if err := json.NewEncoder(w).Encode(roles); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	name, err := GuildRoleName(context.Background(), testClientForServer(srv), "mytoken", "guild1", "role-1")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Admins" {
		t.Fatalf("expected Admins, got %q", name)
	}
}

func TestGuildRoleName_roleNotInGuild(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles := []apiRole{{ID: "role-99", Name: "Other"}}
		if err := json.NewEncoder(w).Encode(roles); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	_, err := GuildRoleName(context.Background(), testClientForServer(srv), "tok", "g1", "missing-role")
	if err == nil || !strings.Contains(err.Error(), "not in guild") {
		t.Fatalf("expected 'not in guild' error, got %v", err)
	}
}

func TestGuildRoleName_serverError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := GuildRoleName(context.Background(), testClientForServer(srv), "tok", "g1", "r1")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status in error, got %v", err)
	}
}

func TestGuildRoleName_missingParams(t *testing.T) {
	t.Parallel()

	_, err := GuildRoleName(context.Background(), nil, "", "g1", "r1")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestGuildChannelNames_success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		channels := []apiChannel{{ID: "ch-1", Name: "general"}, {ID: "ch-2", Name: "raid"}}
		if err := json.NewEncoder(w).Encode(channels); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	got, err := GuildChannelNames(context.Background(), testClientForServer(srv), "tok", "guild1")
	if err != nil {
		t.Fatal(err)
	}
	if got["ch-1"] != "general" || got["ch-2"] != "raid" {
		t.Fatalf("unexpected channel map: %v", got)
	}
}

func TestGuildChannelNames_serverError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	t.Cleanup(srv.Close)

	_, err := GuildChannelNames(context.Background(), testClientForServer(srv), "tok", "g1")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected status in error, got %v", err)
	}
}

func TestGuildChannelNames_missingParams(t *testing.T) {
	t.Parallel()

	_, err := GuildChannelNames(context.Background(), nil, "", "g1")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}
