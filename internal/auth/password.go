// Package auth holds transport-agnostic security primitives: password hashing,
// session tokens, session/CSRF cookies and middleware, and rate limiting.
package auth

import (
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the bcrypt work factor for new hashes.
var BcryptCost = 12

// MaxPasswordBytes is bcrypt's input limit; longer passwords are rejected
// rather than silently truncated.
const MaxPasswordBytes = 72

// ErrPasswordTooLong is returned for passwords over MaxPasswordBytes.
var ErrPasswordTooLong = errors.New("auth: password longer than 72 bytes")

// dummyHash is compared against when a user does not exist, so that login
// takes the same time whether or not the username is valid. It is made on
// first use, at BcryptCost like real hashes.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("extt-dummy-password"), BcryptCost)
	return h
})

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	return string(h), err
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// BurnPasswordCheck spends roughly the time of a CheckPassword call. Use it
// when the user does not exist to avoid leaking that through timing.
func BurnPasswordCheck(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
}
