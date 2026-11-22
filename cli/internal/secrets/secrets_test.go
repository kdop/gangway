package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdop/gangway/shared/designcheck"
)

const goodKey = "tskey-auth-kTESTONLY11CNTRL-abcdefghijklmnopqrstuvwxyz"

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
	got, err := Load(path)
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
	_, err := Load(place(t))
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
		key, err := Load(path)
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
	_, loadErr := Load(open)

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

// UNIT-SECRETS-07. Requirement: KEY-08.
func TestUNITSECRETS07_ProtectedFollowsLinks(t *testing.T) {
	path := place(t)
	if err := Save(path, goodKey); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	link := filepath.Join(t.TempDir(), "innocent.txt")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "settings")
	os.WriteFile(other, []byte("x"), 0o600)

	for candidate, want := range map[string]bool{
		path: true,
		filepath.Join(dir, "..", "gangway", "tailscale-auth-key"): true,
		link:                            true,
		other:                           false,
		filepath.Join(dir, "not-there"): false,
	} {
		if got := Protected(path, candidate); got != want {
			t.Fatalf("Protected(%s) is %v, want %v", candidate, got, want)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SECRETS", "secrets_test.go")
}
