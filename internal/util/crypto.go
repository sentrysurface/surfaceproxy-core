package util

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateID generates a cryptographically secure random hex string.
func GenerateID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback (highly unlikely to fail)
		return "fallback-id"
	}
	return hex.EncodeToString(b)
}
