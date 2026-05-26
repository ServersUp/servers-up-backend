package bnet

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Authenticate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "scope=") {
			http.Error(w, "unexpected scope", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token": "test-token", "token_type": "bearer", "expires_in": 3600}`)
	}))
	defer server.Close()

	client := NewClient("id", "secret")
	client.authURL = server.URL

	err := client.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if client.token != "test-token" {
		t.Errorf("expected token test-token, got %s", client.token)
	}
}

func TestClient_GetConnectedRealmStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": 11, "status": {"type": "UP"}}`)
	}))
	defer server.Close()

	client := NewClient("id", "secret")
	client.token = "fake-token"
	client.apiURL = server.URL + "/%s"

	resp, err := client.GetConnectedRealmStatus(context.Background(), "us", 11, "en_US")
	if err != nil {
		t.Fatalf("GetConnectedRealmStatus failed: %v", err)
	}

	if resp.Status.Type != "UP" {
		t.Errorf("expected status UP, got %s", resp.Status.Type)
	}
}

func TestDefaultWoWRegionEndpoints_krTwRegionalHosts(t *testing.T) {
	t.Parallel()

	kr := DefaultWoWRegionEndpoints()["kr"]
	if kr.APIHost != "kr.api.blizzard.com" || kr.APISubregion != "" {
		t.Fatalf("kr endpoint: %+v", kr)
	}
	tw := DefaultWoWRegionEndpoints()["tw"]
	if tw.APIHost != "tw.api.blizzard.com" || tw.APISubregion != "" {
		t.Fatalf("tw endpoint: %+v", tw)
	}
}

func TestClient_BuildRealmConfigs_krUsesBearerNotQueryToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/data/wow/connected-realm/index" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("access_token") != "" || r.URL.Query().Get("region") != "" {
			http.Error(w, "use regional host and bearer auth", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("namespace") != "dynamic-kr" {
			http.Error(w, "bad namespace", http.StatusBadRequest)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer tok" {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"connected_realms":[]}`)
	}))
	defer srv.Close()

	client := NewClient("id", "secret")
	client.token = "tok"
	client.httpClient = srv.Client()

	ep := DefaultWoWRegionEndpoints()["kr"]
	ep.Scheme = "http"
	ep.APIHost = srv.Listener.Addr().String()

	if _, err := client.BuildRealmConfigs(context.Background(), ep); err != nil {
		t.Fatal(err)
	}
}

func TestClient_BuildRealmConfigs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data/wow/connected-realm/index":
			if r.URL.Query().Get("namespace") != "dynamic-eu" {
				http.Error(w, "bad namespace", http.StatusBadRequest)
				return
			}
			fmt.Fprintf(w, `{"connected_realms":[{"href":"http://%s/data/wow/connected-realm/1305?namespace=dynamic-eu"}]}`, r.Host)
		case "/data/wow/connected-realm/1305":
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"id":1305,"realms":[{"name":"Kazzak","slug":"kazzak"},{"name":"Tarren Mill","slug":"tarren-mill"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient("id", "secret")
	client.token = "tok"
	client.httpClient = srv.Client()

	ep := RegionEndpoint{
		Scheme:    "http",
		APIHost:   srv.Listener.Addr().String(),
		Namespace: "dynamic-eu",
		Locale:    "en_GB",
	}

	got, err := client.BuildRealmConfigs(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("realms: %+v", got)
	}
	if got[0].Slug != "kazzak" || got[0].Name != "Kazzak" || got[0].ConnectedRealmID != 1305 {
		t.Fatalf("first realm: %+v", got[0])
	}
	if got[1].Slug != "tarren-mill" || got[1].Name != "Tarren Mill" {
		t.Fatalf("second realm: %+v", got[1])
	}
}
