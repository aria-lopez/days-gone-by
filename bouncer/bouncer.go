package bouncer

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aria-lopez/days-gone-by/logger"
	"github.com/aria-lopez/days-gone-by/squeal"
)

// Role ;; what a key may do, each role can do everything the ones below it can
type Role int

const (
	RoleViewer Role = iota + 1 // ;; read
	RoleMember                 // ;; + upload and delete images
	RoleAdmin                  // ;; + delete blobs
)

func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "viewer"
	case RoleMember:
		return "member"
	case RoleAdmin:
		return "admin"
	}
	return "unknown"
}

// ParseRole ;; "viewer", "member" or "admin"
func ParseRole(s string) (Role, bool) {
	for _, r := range []Role{RoleViewer, RoleMember, RoleAdmin} {
		if r.String() == s {
			return r, true
		}
	}
	return 0, false
}

// Principal ;; who is making the request
type Principal struct {
	KeyID string
	Name  string
	Role  Role
}

type ctxKey struct{}

// From ;; the Principal that Require put on the request
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// touchEvery ;; last_used_at is written at most this often per key,
// otherwise every GET would turn into a write waiting on our single db connection
const touchEvery = time.Minute

// ignore tokens bigger than this.
const maxTokenLen = 200

var errUnauthenticated = errors.New("missing or invalid API key")

type Bouncer struct {
	db      *squeal.Squeal
	touched sync.Map // ;; key id -> when we last wrote its last_used_at
}

func New(db *squeal.Squeal) *Bouncer {
	return &Bouncer{db: db}
}

// Require ;; wraps a handler. 401 without a valid key, 403 if the keys role is below min.
func (b *Bouncer) Require(min Role, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := b.authenticate(r)
		if errors.Is(err, errUnauthenticated) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="days-gone-by"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if err != nil {
			logger.ErrorF(err, "%s %s internal error during Bouncer.authenticate", r.Method, r.URL.Path)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if p.Role < min {
			http.Error(w, "this API key is not allowed to do that", http.StatusForbidden)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

// authenticate ;; Authorization: Bearer dgb_... to a Principal.
// errUnauthenticated for anything wrong with the key, any other error is our fault
func (b *Bouncer) authenticate(r *http.Request) (Principal, error) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || !strings.HasPrefix(token, keyPrefix) || len(token) > maxTokenLen {
		return Principal{}, errUnauthenticated
	}

	// looked up by hash, so how long the lookup takes tells an attacker nothing
	key, err := b.db.LookupKey(r.Context(), Hash(token))
	if errors.Is(err, squeal.ErrNotFound) {
		return Principal{}, errUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	role, ok := ParseRole(key.Role)
	if !ok {
		return Principal{}, errors.New("key " + key.ID + " has unknown role " + key.Role)
	}

	b.touch(r.Context(), key.ID)
	return Principal{KeyID: key.ID, Name: key.Name, Role: role}, nil
}

// touch ;; updates last_used_at, at most once per touchEvery per key. failing to is not worth failing the request
func (b *Bouncer) touch(ctx context.Context, id string) {
	if last, ok := b.touched.Load(id); ok && time.Since(last.(time.Time)) < touchEvery {
		return
	}
	b.touched.Store(id, time.Now())
	if err := b.db.TouchKey(ctx, id); err != nil {
		logger.ErrorF(err, "couldnt update last_used_at of key %s", id)
	}
}
