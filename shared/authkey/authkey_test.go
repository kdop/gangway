package authkey

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodKey = "tskey-auth-fake-test-only-key"

// place gives the path of a key file in a folder that is not there yet.
func place(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "gangway", "tailscale-auth-key")
}

// ownerOnly fails the test when anyone but the owner has a right on path.
func ownerOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("%s has the rights %o, want none for the group and for others", path, info.Mode().Perm())
	}
}

func wantKey(t *testing.T, path, want string) {
	t.Helper()
	got, err := Load(path, "gangway")
	if err != nil || got != want {
		t.Fatalf("Load gave %q, %v, want %q", got, err, want)
	}
}

// UNIT-SECRETS-01. Requirement: HOST-05.
func TestUNITSECRETS01_SaveMakesTheFolderAndTheFile(t *testing.T) {
	path := place(t)
	if err := Save(path, goodKey); err != nil {
		t.Fatal(err)
	}
	ownerOnly(t, filepath.Dir(path))
	ownerOnly(t, path)
	wantKey(t, path, goodKey)
}

// UNIT-SECRETS-02. Requirement: HOST-05.
func TestUNITSECRETS02_ANewKeyTakesThePlaceOfTheOld(t *testing.T) {
	path := place(t)
	for _, key := range []string{goodKey, goodKey + "-new"} {
		if err := Save(path, key); err != nil {
			t.Fatal(err)
		}
	}
	wantKey(t, path, goodKey+"-new")
	ownerOnly(t, path)
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*"))
	if len(left) != 1 {
		t.Fatalf("the folder holds %v, want the key file only", left)
	}
}

// UNIT-SECRETS-03. Requirement: HOST-04.
func TestUNITSECRETS03_NoFileSaysToRunLogin(t *testing.T) {
	_, err := Load(place(t), "gangway")
	if err == nil || !strings.Contains(err.Error(), "gangway login") {
		t.Fatalf("got %v, want an error that says to run login", err)
	}
}

// UNIT-SECRETS-04. Requirement: HOST-05.
func TestUNITSECRETS04_AFileOthersCanReadIsRefused(t *testing.T) {
	for _, rights := range []os.FileMode{0o640, 0o604, 0o620, 0o644} {
		path := place(t)
		if err := Save(path, goodKey); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, rights); err != nil {
			t.Fatal(err)
		}
		key, err := Load(path, "gangway")
		if err == nil || key != "" {
			t.Fatalf("rights %o: got the key and %v, want an error and no key", rights, err)
		}
		if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatalf("rights %o: the error %q does not name the file and the fix", rights, err)
		}
	}
}

// UNIT-SECRETS-05. Requirement: HOST-05.
func TestUNITSECRETS05_TextThatCannotBeAKeyIsRefused(t *testing.T) {
	for _, key := range []string{
		"", "tskey-auth-", "tskey-auth-abc def", "tskey-auth-abc\x1b[2J", "tskey-auth-abc\n", "tskey-auth-abc​def",
		"hunter2", "tskey-client-abc", "gw_" + strings.Repeat("0", 64),
	} {
		path := place(t)
		if err := Save(path, key); err == nil {
			t.Fatalf("%q was taken as a key", key)
		}
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			t.Fatalf("%q: a folder was made", key)
		}
	}
}

// UNIT-SECRETS-06. Requirement: HOST-05.
func TestUNITSECRETS06_NoErrorHoldsTheKey(t *testing.T) {
	secret := strings.TrimPrefix(goodKey, "tskey-auth-")

	open := place(t)
	if err := Save(open, goodKey); err != nil {
		t.Fatal(err)
	}
	os.Chmod(open, 0o644)
	_, loadErr := Load(open, "gangway")

	locked := t.TempDir()
	os.Chmod(locked, 0o500)
	saveErr := Save(filepath.Join(locked, "gangway", "tailscale-auth-key"), goodKey)

	bad := "tskey-auth-" + secret + " with a space"
	checkErr := Save(place(t), bad)

	for name, err := range map[string]error{"load": loadErr, "save": saveErr, "check": checkErr} {
		if err == nil {
			t.Fatalf("%s gave no error", name)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the error of %s holds the key: %q", name, err)
		}
	}
}

func TestTheFileAndTheLoginBelongToTheProgram(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere")
	for _, program := range []string{"gangway", "gangway-controller"} {
		file, err := File(program)
		if err != nil || file != "/somewhere/"+program+"/tailscale-auth-key" {
			t.Fatalf("the file of %s is %q, %v", program, file, err)
		}
		if _, err := Load(place(t), program); err == nil || !strings.Contains(err.Error(), `"`+program+` login"`) {
			t.Fatalf("with no file, %s is told %v", program, err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if file, _ := File("gangway"); file != "/home/someone/.config/gangway/tailscale-auth-key" {
		t.Fatalf("with no XDG_CONFIG_HOME the file is %q", file)
	}
}

func TestLoadRefusesAFileThatHoldsNoKey(t *testing.T) {
	path := place(t)
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("hunter2\n"), 0o600)
	if key, err := Load(path, "gangway"); err == nil || key != "" || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("got %q, %v, want an error that does not hold the text", key, err)
	}
}

func TestRemove(t *testing.T) {
	path := place(t)
	if err := Remove(path); err != nil {
		t.Fatalf("a file that is not there gave %v", err)
	}
	if err := Save(path, goodKey); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the file is still there")
	}
}

// slowKeys gives what was typed in small pieces, as a keyboard does.
type slowKeys struct{ left string }

func (s *slowKeys) Read(p []byte) (int, error) {
	if s.left == "" {
		return 0, io.EOF
	}
	n := copy(p[:min(3, len(p))], s.left)
	s.left = s.left[n:]
	return n, nil
}

func TestReadHidden(t *testing.T) {
	for typed, want := range map[string]string{
		"abc\r":           "abc",
		"abc\n":           "abc",
		"abc\rdef\r":      "abc",
		"abxx\x7f\x7fc\r": "abc",
		"ab\x08c\r":       "ac",
		"\x7f\x7fabc\r":   "abc",
		"\r":              "",
		"a long pasted key that comes in many pieces\r": "a long pasted key that comes in many pieces",
	} {
		got, err := ReadHidden(&slowKeys{typed})
		if err != nil || got != want {
			t.Errorf("%q gave %q, %v, want %q", typed, got, err, want)
		}
	}
	for _, typed := range []string{"abc\x03", "abc", ""} {
		if got, err := ReadHidden(&slowKeys{typed}); err != ErrStopped || got != "" {
			t.Errorf("%q gave %q, %v, want the stop", typed, got, err)
		}
	}
}

// UNIT-SECRETS-08. Requirement: HOST-06.
func TestUNITSECRETS08_PlainMakesATextSafeToShow(t *testing.T) {
	if got := Plain("invalid key\x1b[2J\r\nnot found é"); got != "invalid key?[2J??not found ?" {
		t.Fatalf("got %q", got)
	}
	long := Plain(strings.Repeat("a", 500))
	if len(long) != 203 || !strings.HasSuffix(long, "...") {
		t.Fatalf("a long text came out %d long", len(long))
	}
}
