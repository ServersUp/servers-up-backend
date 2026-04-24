package bnet

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Authenticate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
