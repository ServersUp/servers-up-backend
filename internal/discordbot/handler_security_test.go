package discordbot

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandleRequest_InvalidSignature(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	resp, _ := f.handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
		Headers: map[string]string{
			"x-signature-ed25519":   "wrong",
			"x-signature-timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		},
		Body: `{"type": 1}`,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad signature, got %d", resp.StatusCode)
	}
}

func TestHandleRequest_StaleSignature(t *testing.T) {
	t.Parallel()
	f := newTestHandlerFixture(t)

	body := `{"type": 1}`
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	sig := hex.EncodeToString(ed25519.Sign(f.priv, []byte(stale+body)))

	resp, _ := f.handler.HandleRequest(context.Background(), events.LambdaFunctionURLRequest{
		Headers: map[string]string{
			"x-signature-ed25519":   sig,
			"x-signature-timestamp": stale,
		},
		Body: body,
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for stale timestamp (crypto valid), got %d", resp.StatusCode)
	}
}
