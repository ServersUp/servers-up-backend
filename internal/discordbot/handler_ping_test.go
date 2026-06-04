package discordbot

import (
	"context"
	"net/http"
	"testing"
)

func TestHandleRequest_Ping(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	resp, err := f.handler.HandleRequest(context.Background(), f.signedRequest(t, `{"type": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Body != `{"type":1}` {
		t.Errorf("expected pong body, got %s", resp.Body)
	}
}
