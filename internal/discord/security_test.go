package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
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

func TestValidateSignatureTimestamp(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()

	t.Run("within skew", func(t *testing.T) {
		t.Parallel()
		ts := strconv.FormatInt(now.Add(2*time.Minute).Unix(), 10)
		if err := ValidateSignatureTimestamp(ts, now, DefaultSignatureMaxSkew); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("at boundary", func(t *testing.T) {
		t.Parallel()
		ts := strconv.FormatInt(now.Add(5*time.Minute).Unix(), 10)
		if err := ValidateSignatureTimestamp(ts, now, DefaultSignatureMaxSkew); err != nil {
			t.Fatalf("expected nil at +5m, got %v", err)
		}
		ts2 := strconv.FormatInt(now.Add(-5*time.Minute).Unix(), 10)
		if err := ValidateSignatureTimestamp(ts2, now, DefaultSignatureMaxSkew); err != nil {
			t.Fatalf("expected nil at -5m, got %v", err)
		}
	})

	t.Run("too old", func(t *testing.T) {
		t.Parallel()
		ts := strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10)
		if err := ValidateSignatureTimestamp(ts, now, DefaultSignatureMaxSkew); err == nil {
			t.Fatal("expected error for stale timestamp")
		}
	})

	t.Run("too new", func(t *testing.T) {
		t.Parallel()
		ts := strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10)
		if err := ValidateSignatureTimestamp(ts, now, DefaultSignatureMaxSkew); err == nil {
			t.Fatal("expected error for future timestamp")
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if err := ValidateSignatureTimestamp("", now, DefaultSignatureMaxSkew); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("not a number", func(t *testing.T) {
		t.Parallel()
		if err := ValidateSignatureTimestamp("abc", now, DefaultSignatureMaxSkew); err == nil {
			t.Fatal("expected error")
		}
	})
}
