package sessions

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

const keyK = "gw_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func session(key string) *Session {
	return &Session{
		KeyHash: messages.KeyHash(key),
		Host:    "web-3",
		Limits:  messages.Limits{CommandSeconds: 60, AnswerSeconds: 60, MaxWaiting: 20},
		Relay:   relay.New(func(messages.Message) {}),
	}
}

func withK(t *testing.T) (*Store, *Session) {
	t.Helper()
	s := NewStore()
	sess := session(keyK)
	if err := s.Add(sess); err != nil {
		t.Fatal(err)
	}
	return s, sess
}

// UNIT-SESSIONS-01. Requirement: KEY-02.
func TestUNITSESSIONS01_FindWithTheKey(t *testing.T) {
	s, sess := withK(t)
	if got := s.Find(keyK); got != sess {
		t.Fatalf("find gave %v, want the session", got)
	}
}

// UNIT-SESSIONS-02. Requirement: KEY-02, SES-01.
func TestUNITSESSIONS02_FindWithAnythingElse(t *testing.T) {
	s, sess := withK(t)
	for _, key := range []string{
		"",
		"gw_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		keyK[:len(keyK)-1],
		keyK + "0",
		strings.TrimPrefix(keyK, messages.KeyPrefix),
		sess.KeyHash,
		messages.KeyPrefix + sess.KeyHash,
		" " + keyK,
		strings.ToUpper(keyK),
	} {
		if got := s.Find(key); got != nil {
			t.Errorf("find with %q gave a session", key)
		}
	}

	// A key with no gw_ finds nothing, even when a session has its hash.
	bare := strings.TrimPrefix(keyK, messages.KeyPrefix)
	odd := session(bare)
	if err := s.Add(odd); err != nil {
		t.Fatal(err)
	}
	if s.Find(bare) != nil {
		t.Error("find with a key that has no gw_ gave a session")
	}
}

// UNIT-SESSIONS-03. Requirement: KEY-03, SES-03.
func TestUNITSESSIONS03_RemovedMeansDead(t *testing.T) {
	s, sess := withK(t)
	s.Remove(sess)
	if s.Find(keyK) != nil {
		t.Fatal("find gave a removed session")
	}
	if s.Count() != 0 {
		t.Fatalf("%d sessions are live, want 0", s.Count())
	}
}

// UNIT-SESSIONS-04. Requirement: KEY-09.
func TestUNITSESSIONS04_OneLiveSessionForOneHash(t *testing.T) {
	s, first := withK(t)
	if err := s.Add(session(keyK)); err != ErrTaken {
		t.Fatalf("the second add gave %v, want ErrTaken", err)
	}
	if s.Find(keyK) != first {
		t.Fatal("find no longer gives the first session")
	}
}

// UNIT-SESSIONS-05. Requirement: KEY-09.
func TestUNITSESSIONS05_AHashHasOneForm(t *testing.T) {
	s := NewStore()
	good := messages.KeyHash(keyK)
	for _, hash := range []string{
		"",
		good[:63],
		good + "0",
		strings.ToUpper(good),
		"g" + good[1:],
		good[:32],
		keyK,
	} {
		sess := session(keyK)
		sess.KeyHash = hash
		if err := s.Add(sess); err != ErrBadHash {
			t.Errorf("add with the hash %q gave %v, want ErrBadHash", hash, err)
		}
	}
	if s.Count() != 0 {
		t.Fatalf("%d sessions are live, want 0", s.Count())
	}
}

// UNIT-SESSIONS-06. Requirement: KEY-03.
func TestUNITSESSIONS06_RemovingAnOldSessionLeavesANewOne(t *testing.T) {
	s, a := withK(t)
	s.Remove(a)
	b := session(keyK)
	if err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	s.Remove(a)
	if s.Find(keyK) != b {
		t.Fatal("removing the old session took out the new one")
	}
}

// UNIT-SESSIONS-07. Requirement: SES-07.
func TestUNITSESSIONS07_ANewStoreIsEmpty(t *testing.T) {
	s := NewStore()
	if s.Count() != 0 || s.Find(keyK) != nil {
		t.Fatal("a new store holds a session")
	}
}

// UNIT-SESSIONS-08. Requirement: SES-06.
func TestUNITSESSIONS08_TheKeyIsNotHeld(t *testing.T) {
	_, sess := withK(t)
	secret := strings.TrimPrefix(keyK, messages.KeyPrefix)
	if all := fmt.Sprintf("%+v", *sess); strings.Contains(all, secret) {
		t.Fatalf("the session holds the key: %s", all)
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SESSIONS", "sessions_test.go")
}
