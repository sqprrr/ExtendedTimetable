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

// inviteAlphabet avoids look-alike characters (0/O, 1/I/L).
const inviteAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewInviteCode returns a random invite code like "K7QX-M2PA-R9TB".
func NewInviteCode() string {
	const groups, perGroup = 3, 4
	out := make([]byte, 0, groups*perGroup+groups-1)
	for i := range groups * perGroup {
		if i > 0 && i%perGroup == 0 {
			out = append(out, '-')
		}
		out = append(out, inviteAlphabet[randIntn(len(inviteAlphabet))])
	}
	return string(out)
}

// randIntn returns a uniform random int in [0, n) for small n.
func randIntn(n int) int {
	limit := 256 - 256%n
	var b [1]byte
	for {
		rand.Read(b[:])
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}
