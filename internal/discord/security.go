package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// DefaultSignatureMaxSkew is the maximum allowed clock skew for
// x-signature-timestamp when validating Discord interactions (± this duration).
const DefaultSignatureMaxSkew = 5 * time.Minute

// ValidateSignatureTimestamp rejects replayed requests when Discord's
// x-signature-timestamp is not within now±maxSkew. Discord sends Unix time in seconds.
func ValidateSignatureTimestamp(timestamp string, now time.Time, maxSkew time.Duration) error {
	if timestamp == "" {
		return fmt.Errorf("empty signature timestamp")
	}
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid signature timestamp: %w", err)
	}
	ts := time.Unix(sec, 0).UTC()
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	if delta > maxSkew {
		return fmt.Errorf("signature timestamp outside allowed skew")
	}
	return nil
}

// VerifySignature validates that a request came from Discord using Ed25519.
// Discord sends a signature and a timestamp which must be combined with the body.
func VerifySignature(publicKeyHex, signatureHex, timestamp, body string) error {
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return fmt.Errorf("failed to decode public key: %w", err)
	}

	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length")
	}

	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature length")
	}

	// Discord's signature is verified against: timestamp + body
	message := []byte(timestamp + body)
	if !ed25519.Verify(publicKey, message, signature) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
