package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewToken returns a random 256-bit URL-safe token.
func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b) // never returns an error
	return base64.RawURLEncoding.EncodeToString(b)
}

// SessionID derives the database key for a session token. Only the hash is
// stored, so a leaked database does not leak live sessions.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
