package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	publicKeyHex := hex.EncodeToString(pub)

	timestamp := "1612345678"
	body := "{\"type\": 1}"
	message := []byte(timestamp + body)
	signature := ed25519.Sign(priv, message)
	signatureHex := hex.EncodeToString(signature)

	t.Run("Valid Signature", func(t *testing.T) {
		err := VerifySignature(publicKeyHex, signatureHex, timestamp, body)
		if err != nil {
			t.Errorf("expected signature to be valid, got error: %v", err)
		}
	})

	t.Run("Invalid Body", func(t *testing.T) {
		err := VerifySignature(publicKeyHex, signatureHex, timestamp, body+"extra")
		if err == nil {
			t.Error("expected error for invalid body, got nil")
		}
	})

	t.Run("Invalid Timestamp", func(t *testing.T) {
		err := VerifySignature(publicKeyHex, signatureHex, "wrong", body)
		if err == nil {
			t.Error("expected error for invalid timestamp, got nil")
		}
	})

	t.Run("Invalid Public Key", func(t *testing.T) {
		err := VerifySignature("invalid", signatureHex, timestamp, body)
		if err == nil {
			t.Error("expected error for invalid public key, got nil")
		}
	})
}
