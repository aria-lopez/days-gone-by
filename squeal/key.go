package squeal

import "errors"

// ErrLastAdmin ;; revoking this key would leave nobody able to manage the server
var ErrLastAdmin = errors.New("that is the last active admin key")

// Key ;; an api key as stored, the hash and the key itself never leave the db layer
type Key struct {
	ID         string
	Name       string
	Prefix     string
	Role       string
	CreatedAt  int64
	LastUsedAt *int64 // ;; nil = never used
	RevokedAt  *int64 // ;; nil = active
}
