package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes gives 256 bits of entropy — far beyond guessing range.
const tokenBytes = 32

// NewToken returns a URL-safe opaque token and the hash to store. The plaintext is
// returned once and never persisted: a database copy must not yield a usable session.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken derives the stored form of a token. SHA-256 is correct here rather than a
// password KDF: the input already has full entropy, so slow hashing buys nothing.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
