// Package sessions holds the live sessions, in memory only.
// The design is in design.md.
package sessions

import (
	"errors"
	"strings"
	"sync"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/shared/messages"
)

// Session is one live session. It holds the hash of its key, never the key.
type Session struct {
	KeyHash string
	Host    string
	Limits  messages.Limits
	Relay   *relay.Relay
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
