package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

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

// UNIT-SESSIONS-09. Requirement: KEY-05.
func TestUNITSESSIONS09_TheFirstClaimGetsTheToken(t *testing.T) {
	_, a := withK(t)
	token, ok := a.Claim()
	raw, err := hex.DecodeString(strings.TrimPrefix(token, messages.CallerPrefix))
	if !ok || !strings.HasPrefix(token, messages.CallerPrefix) || err != nil || len(raw)*8 != 256 {
		t.Fatalf("the first claim gave %q, %v: want the prefix and 256 bits in hex", token, ok)
	}
	if again, ok := a.Claim(); ok || again != "" {
		t.Fatalf("a second claim gave %q, %v: want nothing", again, ok)
	}
	if !a.Owns(token) {
		t.Fatal("the second claim changed the owner")
	}
	b := session(keyK)
	if other, _ := b.Claim(); other == token {
		t.Fatal("two sessions got the same token")
	}
}

// UNIT-SESSIONS-10. Requirement: KEY-05.
func TestUNITSESSIONS10_OwnsOnlyWithTheWholeToken(t *testing.T) {
	_, a := withK(t)
	for _, guess := range []string{"", messages.CallerPrefix, messages.CallerPrefix + strings.Repeat("0", 64)} {
		if a.Owns(guess) {
			t.Fatalf("before a claim, %q owns the session", guess)
		}
	}
	token, _ := a.Claim()
	b := session(keyK)
	other, _ := b.Claim()
	if !a.Owns(token) {
		t.Fatal("the token does not own its session")
	}
	sum := sha256.Sum256([]byte(token))
	for _, guess := range []string{
		"",
		token[:len(token)-1],
		token + "0",
		strings.TrimPrefix(token, messages.CallerPrefix),
		strings.ToUpper(token),
		other,
		keyK,
		hex.EncodeToString(sum[:]),
	} {
		if a.Owns(guess) {
			t.Fatalf("%q owns the session", guess)
		}
	}
}

// UNIT-SESSIONS-11. Requirement: KEY-05.
func TestUNITSESSIONS11_ManyClaimsAtOnceGiveOneToken(t *testing.T) {
	_, a := withK(t)
	const callers = 100
	got := make(chan string, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			token, _ := a.Claim()
			got <- token
		}()
	}
	close(start)
	var tokens []string
	for i := 0; i < callers; i++ {
		if token := <-got; token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != 1 || !a.Owns(tokens[0]) {
		t.Fatalf("%d callers got a token, want exactly one, and it must own the session", len(tokens))
	}
}

// UNIT-SESSIONS-12. Requirement: KEY-05, SES-06.
func TestUNITSESSIONS12_TheTokenIsNotHeld(t *testing.T) {
	_, sess := withK(t)
	token, _ := sess.Claim()
	secret := strings.TrimPrefix(token, messages.CallerPrefix)
	raw, _ := hex.DecodeString(secret)
	// Every field as text, and the one field of bytes as it is and in hex.
	all := fmt.Sprintf("%+v %s %x", *sess, sess.callerHash, sess.callerHash)
	if strings.Contains(all, secret) || strings.Contains(all, string(raw)) {
		t.Fatalf("the session holds the caller token: %s", all)
	}
}

// UNIT-SESSIONS-13. Requirement: KEY-05, KEY-03.
func TestUNITSESSIONS13_TheTokenDiesWithTheSession(t *testing.T) {
	s, a := withK(t)
	token, _ := a.Claim()
	s.Remove(a)
	b := session(keyK)
	if err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	if s.Find(keyK).Owns(token) {
		t.Fatal("the token of an ended session owns the new session with the same key hash")
	}
	if fresh, ok := s.Find(keyK).Claim(); !ok || fresh == token {
		t.Fatal("the new session has no claim of its own to give")
	}
}

// wantTime checks what the session tells about its time.
func wantTime(t *testing.T, sess *Session, now time.Time, seconds int, up bool) {
	t.Helper()
	if gotSeconds, gotUp := sess.Time(now); gotSeconds != seconds || gotUp != up {
		t.Fatalf("the session has %d seconds left and time up is %v, want %d and %v", gotSeconds, gotUp, seconds, up)
	}
}

// UNIT-SESSIONS-14. Requirement: LEN-01.
func TestUNITSESSIONS14_TheSecondsLeftCountDown(t *testing.T) {
	_, sess := withK(t)
	start := time.Unix(1000, 0)
	sess.Counts(start, 30*time.Minute)
	wantTime(t, sess, start, 1800, false)
	wantTime(t, sess, start.Add(10*time.Minute), 1200, false)
	wantTime(t, sess, start.Add(30*time.Minute-500*time.Millisecond), 1, false)
	wantTime(t, sess, start.Add(30*time.Minute), 0, false)
	wantTime(t, sess, start.Add(90*time.Minute), 0, false)
}

// UNIT-SESSIONS-15. Requirement: LEN-03, LEN-06.
func TestUNITSESSIONS15_TimeUpAndExtended(t *testing.T) {
	_, sess := withK(t)
	start := time.Unix(1000, 0)
	sess.Counts(start, 30*time.Minute)
	now := start.Add(30 * time.Minute)
	sess.TimeIsUp()
	wantTime(t, sess, now, 0, true)
	wantTime(t, sess, start, 0, true)
	sess.Extended(now, 600*time.Second)
	wantTime(t, sess, now, 600, false)
	sess.Extended(now, -600*time.Second)
	wantTime(t, sess, now, 0, false)
}

// UNIT-SESSIONS-16. Requirement: LEN-09.
func TestUNITSESSIONS16_TheTimeOfOneSessionIsNotTheTimeOfAnother(t *testing.T) {
	_, one := withK(t)
	other := session(keyK)
	start := time.Unix(1000, 0)
	one.Counts(start, 30*time.Minute)
	other.Counts(start, 30*time.Minute)
	one.Extended(start, time.Hour)
	wantTime(t, other, start, 1800, false)
	one.TimeIsUp()
	wantTime(t, other, start, 1800, false)
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SESSIONS", "sessions_test.go")
}
