// Package sessions holds the live sessions, in memory only.
// The design is in design.md.
package sessions

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/shared/messages"
)

// Session is one live session. It holds the hash of its key, never the
// key, and the hash of its caller token, never the token.
type Session struct {
	KeyHash string
	Host    string
	Limits  messages.Limits
	Relay   *relay.Relay

	// callerHash is the SHA-256 of the caller token. Nil: no caller has
	// claimed the key yet. Guarded by callerMu.
	callerHash []byte
}

// callerBits is how many random bits a caller token holds: as many as a
// session key (SESSION_KEY_BITS).
const callerBits = 256

// callerMu guards the callerHash of every session. A claim is rare and
// short, so one lock for all is enough.
var callerMu sync.Mutex

// Claim makes the caller the owner of the key. The first claim on a
// session gives a new random token. Every later one gives nothing: the
// token cannot be handed out twice.
func (sess *Session) Claim() (token string, ok bool) {
	callerMu.Lock()
	defer callerMu.Unlock()
	if sess.callerHash != nil {
		return "", false
	}
	raw := make([]byte, callerBits/8)
	if _, err := rand.Read(raw); err != nil {
		return "", false
	}
	token = messages.CallerPrefix + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	sess.callerHash = sum[:]
	return token, true
}

// Owns tells whether this is the caller token of the session. Before a
// claim, no token is.
func (sess *Session) Owns(token string) bool {
	sum := sha256.Sum256([]byte(token))
	callerMu.Lock()
	defer callerMu.Unlock()
	if sess.callerHash == nil {
		return false
	}
	// The time this takes must not tell how much of a guess was right.
	return subtle.ConstantTimeCompare(sum[:], sess.callerHash) == 1
}

var (
	ErrBadHash = errors.New("the key hash is not a SHA-256 in hex")
	ErrTaken   = errors.New("a live session already has this key hash")
)

// Store is safe for use from many goroutines. Nothing in it is ever
// written to disk.
type Store struct {
	mu     sync.Mutex
	byHash map[string]*Session
}

func NewStore() *Store {
	return &Store{byHash: map[string]*Session{}}
}

// Add makes the session live.
func (s *Store) Add(sess *Session) error {
	if !validHash(sess.KeyHash) {
		return ErrBadHash
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byHash[sess.KeyHash] != nil {
		return ErrTaken
	}
	s.byHash[sess.KeyHash] = sess
	return nil
}

// Find gives the live session of this key, or nil.
func (s *Store) Find(key string) *Session {
	if !strings.HasPrefix(key, messages.KeyPrefix) {
		return nil
	}
	hash := messages.KeyHash(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byHash[hash]
}

// Remove ends the session: its key is dead from this moment.
func (s *Store) Remove(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byHash[sess.KeyHash] == sess {
		delete(s.byHash, sess.KeyHash)
	}
}

// Count tells how many sessions are live.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byHash)
}

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
