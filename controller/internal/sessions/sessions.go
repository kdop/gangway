// Package sessions holds the live sessions, in memory only.
// The design is in docs/design.md.
package sessions

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

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

	// The session's time, as the host told it. Guarded by timeMu.
	endsAt time.Time
	timeUp bool
}

// timeMu guards the time of every session. It is held for a moment only.
var timeMu sync.Mutex

// Counts starts the count of the session's time: the host said the
// session lasts this long from now.
func (sess *Session) Counts(now time.Time, length time.Duration) {
	timeMu.Lock()
	defer timeMu.Unlock()
	sess.endsAt = now.Add(length)
}

// TimeIsUp notes that the host said its time is up.
func (sess *Session) TimeIsUp() {
	timeMu.Lock()
	defer timeMu.Unlock()
	sess.timeUp = true
}

// Extended notes that the host said its user extended the session, and
// that it has this long left from now.
func (sess *Session) Extended(now time.Time, left time.Duration) {
	timeMu.Lock()
	defer timeMu.Unlock()
	sess.endsAt = now.Add(max(left, 0))
	sess.timeUp = false
}

// Time tells how many seconds the session has left, and whether its time
// is up. Both are what the host said: the controller decides neither.
func (sess *Session) Time(now time.Time) (secondsLeft int, timeUp bool) {
	timeMu.Lock()
	defer timeMu.Unlock()
	if sess.timeUp {
		return 0, true
	}
	left := sess.endsAt.Sub(now)
	if left <= 0 {
		return 0, false
	}
	return int((left + time.Second - 1) / time.Second), false
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
	// Clock tells the time. Nil: the system's time. A test puts its own
	// clock here.
	Clock func() time.Time

	mu     sync.Mutex
	byHash map[string]*Session
}

func NewStore() *Store {
	return &Store{byHash: map[string]*Session{}}
}

// Now tells the time by the store's clock.
func (s *Store) Now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
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
