package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

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
