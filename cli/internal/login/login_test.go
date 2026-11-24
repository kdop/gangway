package login

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdop/gangway/cli/internal/network"
	"github.com/kdop/gangway/cli/internal/secrets"
	"github.com/kdop/gangway/shared/designcheck"
)

const (
	oldKey = "tskey-auth-fake-old-key"
	newKey = "tskey-auth-fake-new-key"
)

// world is one login with a keyboard and a screen played by the test.
type world struct {
	file   string
	screen bytes.Buffer
	tried  []string
	answer error // what Tailscale says to the key
	check  error
}

func newWorld(t *testing.T) *world {
	return &world{file: filepath.Join(t.TempDir(), "gangway", "tailscale-auth-key")}
}

// run does a login in which the user types this text.
func (w *world) run(typed string) error {
	return Run(context.Background(), Parts{
		Check:  func() error { return w.check },
		Keys:   strings.NewReader(typed),
		Screen: &w.screen,
		File:   w.file,
		Try: func(_ context.Context, key string) error {
			w.tried = append(w.tried, key)
			return w.answer
		},
	})
}

func (w *world) stored(t *testing.T) string {
	t.Helper()
	key, err := secrets.Load(w.file)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (w *world) noFile(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(w.file); err == nil {
		t.Fatal("a key file is there")
	}
}

// UNIT-LOGIN-01. Requirement: HOST-04.
func TestUNITLOGIN01_AGoodKeyIsStoredAndTried(t *testing.T) {
	w := newWorld(t)
	if err := w.run(newKey + "\r"); err != nil {
		t.Fatal(err)
	}
	if w.stored(t) != newKey {
		t.Fatal("the key is not the one stored")
	}
	if len(w.tried) != 1 || w.tried[0] != newKey {
		t.Fatalf("Tailscale was tried with %d keys, want the new one once", len(w.tried))
	}
	if !strings.Contains(w.screen.String(), "This host is ready") {
		t.Fatalf("the screen does not say the host is ready: %q", w.screen.String())
	}
}

// UNIT-LOGIN-02. Requirement: HOST-05.
func TestUNITLOGIN02_TheKeyIsNeverShown(t *testing.T) {
	secret := strings.TrimPrefix(newKey, "tskey-auth-")
	for _, answer := range []error{nil, network.ErrKeyRefused, errors.New("no network")} {
		w := newWorld(t)
		w.answer = answer
		err := w.run(newKey + "\r")
		shown := w.screen.String()
		if err != nil {
			shown += err.Error()
		}
		if strings.Contains(shown, secret) {
			t.Fatalf("the key is on the screen or in the error: %q", shown)
		}
	}
}

// UNIT-LOGIN-03. Requirement: HOST-05.
func TestUNITLOGIN03_ARefusedKeyLeavesTheOldOne(t *testing.T) {
	w := newWorld(t)
	if err := secrets.Save(w.file, oldKey); err != nil {
		t.Fatal(err)
	}
	w.answer = network.ErrKeyRefused
	err := w.run(newKey + "\r")
	if err == nil || !strings.Contains(err.Error(), "Tailscale refused the key") {
		t.Fatalf("got %v, want an error that says Tailscale refused the key", err)
	}
	if w.stored(t) != oldKey {
		t.Fatal("the old key is not the one stored")
	}
}

// UNIT-LOGIN-04. Requirement: HOST-05.
func TestUNITLOGIN04_ARefusedKeyIsNotKept(t *testing.T) {
	w := newWorld(t)
	w.answer = network.ErrKeyRefused
	if err := w.run(newKey + "\r"); err == nil {
		t.Fatal("no error")
	}
	w.noFile(t)
}

// UNIT-LOGIN-05. Requirement: HOST-04.
func TestUNITLOGIN05_AKeyThatCouldNotBeTriedIsKept(t *testing.T) {
	w := newWorld(t)
	w.answer = errors.New("could not get onto the Tailscale network in 30s")
	if err := w.run(newKey + "\r"); err != nil {
		t.Fatal(err)
	}
	if w.stored(t) != newKey {
		t.Fatal("the key is not stored")
	}
	if !strings.Contains(w.screen.String(), "could not be tried") {
		t.Fatalf("the screen does not say the key could not be tried: %q", w.screen.String())
	}
}

// UNIT-LOGIN-06. Requirement: HOST-05.
func TestUNITLOGIN06_TextThatCannotBeAKeyIsRefused(t *testing.T) {
	for _, typed := range []string{"hunter2\r", "\r", "tskey-auth-with a space\r", "tskey-auth-abc\x03", "tskey-auth-abc"} {
		w := newWorld(t)
		if err := w.run(typed); err == nil {
			t.Fatalf("%q gave no error", typed)
		}
		w.noFile(t)
		if len(w.tried) != 0 {
			t.Fatalf("%q: Tailscale was tried", typed)
		}
	}
	// A slip that is taken back with the backspace key does no harm.
	w := newWorld(t)
	if err := w.run(newKey + "xx\x7f\x7f\n"); err != nil || w.stored(t) != newKey {
		t.Fatalf("a key typed with a slip and two backspaces was not taken: %v", err)
	}
}

// UNIT-LOGIN-07. Requirement: HOST-01, HOST-02.
func TestUNITLOGIN07_RootAndNoTerminalAreRefused(t *testing.T) {
	w := newWorld(t)
	w.check = errors.New("gangway does not run as root")
	if err := w.run(newKey + "\r"); err != w.check {
		t.Fatalf("got %v, want the error of the check", err)
	}
	if w.screen.Len() != 0 || len(w.tried) != 0 {
		t.Fatalf("the login went on: screen %q", w.screen.String())
	}
	w.noFile(t)
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-LOGIN", "login_test.go")
}
