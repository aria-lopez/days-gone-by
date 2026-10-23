// Package bouncer decides who gets in: api keys and the middleware that checks them.
package bouncer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// prefix to make identifying the keys easy (incase of leaks etc;)
const keyPrefix = "dgb_"

// NewKey ;; gen a key, only displayed once and never stored -- we store its encoded form
func NewKey() (token string, hash string, prefix string) {
	b := make([]byte, 32) // 256 bits
	rand.Read(b)          // cant fail since go 1.24, it crashes the program instead of returning
	token = keyPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, Hash(token), token[:len(keyPrefix)+6]
}

// Hash; hex of key
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
