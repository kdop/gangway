package session

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

const address = "http://127.0.0.1:8642"

func mustNew(t *testing.T) *Session {
	t.Helper()
	s, err := New(256)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAddress(address); err != nil {
		t.Fatal(err)
	}
	return s
}

// UNIT-SESSION-01. Requirement: KEY-01.
func TestUNITSESSION01_TheKeyHoldsTheBitsAskedFor(t *testing.T) {
	for bits, hexLen := range map[int]int{128: 32, 256: 64, 512: 128} {
		s, err := New(bits)
		if err != nil {
			t.Fatal(err)
		}
		want := regexp.MustCompile(`^gw_[0-9a-f]{` + strconv.Itoa(hexLen) + `}$`)
		if !want.MatchString(s.Key()) {
			t.Fatalf("with %d bits the key is %q, want gw_ and %d hex characters", bits, s.Key(), hexLen)
		}
	}
}

// UNIT-SESSION-02. Requirement: KEY-01, SES-03.
func TestUNITSESSION02_EveryKeyIsNew(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		key := mustNew(t).Key()
		if seen[key] {
			t.Fatalf("the key %q came twice", key)
		}
		seen[key] = true
	}
}

type brokenRandom struct{ give int }

func (b brokenRandom) Read(p []byte) (int, error) {
	n := min(b.give, len(p))
	return n, errors.New("the random source failed")
}

// UNIT-SESSION-03. Requirement: KEY-01.
func TestUNITSESSION03_NoRandomBytesNoSession(t *testing.T) {
	for _, give := range []int{0, 5, 31} {
		s, err := newFrom(brokenRandom{give}, 256)
		if err == nil || s != nil {
			t.Fatalf("a random source that gave %d bytes made a session", give)
		}
	}
}

// UNIT-SESSION-04. Requirement: KEY-01.
func TestUNITSESSION04_AWeakKeyLengthIsRefused(t *testing.T) {
	for _, bits := range []int{-8, 0, 8, 64, 120, 127, 129, 130, 255} {
		if s, err := New(bits); err == nil {
			t.Fatalf("%d bits made a session with the key %q", bits, s.Key())
		}
	}
}

// UNIT-SESSION-05. Requirement: KEY-04.
func TestUNITSESSION05_ThePasteLine(t *testing.T) {
	s := mustNew(t)
	if got, want := s.PasteLine(), "/gangway "+address+" "+s.Key(); got != want {
		t.Fatalf("the paste line is %q, want %q", got, want)
	}
	parts := strings.Split(s.PasteLine(), " ")
	if len(parts) != 3 || parts[0] != "/gangway" || parts[1] != address || !strings.HasPrefix(parts[2], "gw_") {
		t.Fatalf("the paste line %q does not split into the skill, the address and the key", s.PasteLine())
	}
}

// UNIT-SESSION-06. Requirement: KEY-04, KEY-10.
func TestUNITSESSION06_AnAddressThatBreaksThePasteLineIsRefused(t *testing.T) {
	for _, bad := range []string{
		"", "http://a b", "http://a\tb", "http://a\nb", "http://a\x1b[2Jb", "http://a b",
		"example.com", "ftp://example.com", "http://", "http://a​b", "http://a‮b",
	} {
		s, err := New(256)
		if err != nil {
			t.Fatal(err)
		}
		err = s.SetAddress(bad)
		if err != ErrBadAddress {
			t.Fatalf("the address %q gave %v, and the paste line %q", bad, err, s.PasteLine())
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("the error holds the bad address %q", bad)
		}
	}
}

// UNIT-SESSION-07. Requirement: SES-03.
func TestUNITSESSION07_AnEndedSessionStaysEnded(t *testing.T) {
	s := mustNew(t)
	if s.State() != Live || !s.Takes() {
		t.Fatal("a new session is not live")
	}
	s.End()
	if s.State() != Ended || s.Takes() {
		t.Fatal("the session is live after its end")
	}
	s.End()
	_, _, _ = s.Key(), s.KeyHash(), s.PasteLine()
	if s.State() != Ended || s.Takes() {
		t.Fatal("the session came back to life")
	}
}

// UNIT-SESSION-08. Requirement: SES-06.
func TestUNITSESSION08_TheHashIsNotTheKey(t *testing.T) {
	s := mustNew(t)
	if got, want := s.KeyHash(), messages.KeyHash(s.Key()); got != want {
		t.Fatalf("the hash is %q, want %q", got, want)
	}
	secret := strings.TrimPrefix(s.Key(), messages.KeyPrefix)
	if s.KeyHash() == secret || strings.Contains(s.KeyHash(), messages.KeyPrefix) {
		t.Fatal("the hash holds the key")
	}
}

// UNIT-SESSION-09. Requirement: SES-06.
func TestUNITSESSION09_ThePackageCannotWriteAFile(t *testing.T) {
	allowed := map[string]bool{
		"crypto/rand": true, "encoding/hex": true, "errors": true, "fmt": true, "io": true, "time": true,
		"github.com/kdop/gangway/shared/messages": true,
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[path] {
				t.Errorf("%s imports %q, which is not on the list of packages that cannot write a file", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no Go file to check")
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SESSION", "session_test.go", "clock_test.go")
}
