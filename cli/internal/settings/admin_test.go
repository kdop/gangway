package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// me is the user who runs the tests. The tests hand it to loadAdmin as the
// owner, in the place of root.
var me = uint32(os.Geteuid())

// adminFolder makes an admin folder with this text in its file. An empty
// text: no file.
func adminFolder(t *testing.T, text string) string {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "gangway")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if text != "" {
		if err := os.WriteFile(filepath.Join(folder, "settings"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// wantAdminError checks that the folder gives an error that holds each of
// these words, and no bound.
func wantAdminError(t *testing.T, folder string, owner uint32, words ...string) {
	t.Helper()
	got, err := loadAdmin(folder, owner)
	if err == nil {
		t.Fatalf("no error for %s", folder)
	}
	for _, w := range words {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("the error %q does not hold %q", err, w)
		}
	}
	if got.SessionMax != 0 {
		t.Fatalf("a bound from a file that was refused is used: %+v", got)
	}
}

// UNIT-SETTINGS-15. Requirement: SET-01, LEN-05.
func TestUNITSETTINGS15_TheAdminFileSetsTheMaximum(t *testing.T) {
	folder := adminFolder(t, "# the longest a session may last\n\n  SESSION_MAX = 20m  \n")
	got, err := loadAdmin(folder, me)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionMax != 20*time.Minute || got.File != filepath.Join(folder, "settings") {
		t.Fatalf("got %+v, want 20 minutes from the file in the folder", got)
	}
}

// UNIT-SETTINGS-16. Requirement: SET-03.
func TestUNITSETTINGS16_NoAdminFileNoBound(t *testing.T) {
	for name, folder := range map[string]string{
		"no folder":          filepath.Join(t.TempDir(), "gangway"),
		"a folder, no file":  adminFolder(t, ""),
		"a file with no set": adminFolder(t, "# nothing is set\n"),
	} {
		got, err := loadAdmin(folder, me)
		if err != nil || got.SessionMax != 0 {
			t.Fatalf("%s: got %+v, %v, want no bound and no error", name, got, err)
		}
		if length, cut := got.Length(100 * time.Hour); length != 100*time.Hour || cut {
			t.Fatalf("%s: a length was cut with no maximum", name)
		}
	}
}

// UNIT-SETTINGS-17. Requirement: SET-05.
func TestUNITSETTINGS17_AMistakeInTheAdminFile(t *testing.T) {
	for text, words := range map[string][]string{
		"SESSION_LENGTH = 10m\n":                {"line 1", "SESSION_LENGTH"},
		"SESSION_MAX = 20m\nCOMMAND_SHELL=/x\n": {"line 2", "COMMAND_SHELL"},
		"SESSION_MAX = 20m\nNO_SUCH = 1\n":      {"line 2", "NO_SUCH"},
		"SESSION_MAX = 0s\n":                    {"line 1", "SESSION_MAX"},
		"SESSION_MAX = -5m\n":                   {"line 1", "SESSION_MAX"},
		"SESSION_MAX = 20\n":                    {"line 1", "SESSION_MAX"},
		"SESSION_MAX = \n":                      {"line 1", "SESSION_MAX"},
		"\nSESSION_MAX 20m\n":                   {"line 2", "="},
		"SESSION_MAX = 20m\nSESSION_MAX = 5m\n": {"line 2", "twice"},
	} {
		folder := adminFolder(t, text)
		wantAdminError(t, folder, me, append(words, filepath.Join(folder, "settings"))...)
	}
}

// UNIT-SETTINGS-18. Requirement: SET-05.
func TestUNITSETTINGS18_TheAdminFileMustBelongToItsOwner(t *testing.T) {
	folder := adminFolder(t, "SESSION_MAX = 20m\n")
	file := filepath.Join(folder, "settings")
	wantAdminError(t, folder, me+1, folder, "owned by")
	wantAdminError(t, folder, 0, folder, "owned by root")

	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := trusted(file, info, me); err != nil {
		t.Fatalf("the user's own file is not trusted: %v", err)
	}
	for owner, word := range map[uint32]string{me + 1: "owned by", 0: "owned by root"} {
		if err := trusted(file, info, owner); err == nil || !strings.Contains(err.Error(), word) || !strings.Contains(err.Error(), file) {
			t.Fatalf("a file of another owner gave %v", err)
		}
	}
}

// UNIT-SETTINGS-19. Requirement: SET-05.
func TestUNITSETTINGS19_NobodyElseMayWriteTheAdminFile(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		folder := adminFolder(t, "SESSION_MAX = 20m\n")
		if err := os.Chmod(folder, mode); err != nil {
			t.Fatal(err)
		}
		wantAdminError(t, folder, me, folder, "writable by")
	}
	for _, mode := range []os.FileMode{0o664, 0o646, 0o666} {
		folder := adminFolder(t, "SESSION_MAX = 20m\n")
		file := filepath.Join(folder, "settings")
		if err := os.Chmod(file, mode); err != nil {
			t.Fatal(err)
		}
		wantAdminError(t, folder, me, file, "writable by")
	}
	for _, mode := range []os.FileMode{0o644, 0o444, 0o600} {
		folder := adminFolder(t, "SESSION_MAX = 20m\n")
		if err := os.Chmod(filepath.Join(folder, "settings"), mode); err != nil {
			t.Fatal(err)
		}
		if got, err := loadAdmin(folder, me); err != nil || got.SessionMax != 20*time.Minute {
			t.Fatalf("with the rights %o: got %+v, %v", mode, got, err)
		}
	}

	if me != 0 {
		folder := adminFolder(t, "SESSION_MAX = 20m\n")
		file := filepath.Join(folder, "settings")
		if err := os.Chmod(file, 0); err != nil {
			t.Fatal(err)
		}
		wantAdminError(t, folder, me, file, "cannot read")
	}

	// A folder in the place of the file, and a link in the place of the
	// folder.
	folder := adminFolder(t, "")
	if err := os.Mkdir(filepath.Join(folder, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantAdminError(t, folder, me, "plain file")
	link := filepath.Join(t.TempDir(), "gangway")
	if err := os.Symlink(adminFolder(t, "SESSION_MAX = 20m\n"), link); err != nil {
		t.Fatal(err)
	}
	wantAdminError(t, link, me, link, "must be a folder")
}

// UNIT-SETTINGS-20. Requirement: SET-01, SET-04.
func TestUNITSETTINGS20_ALengthAboveTheMaximumIsCut(t *testing.T) {
	a := Admin{SessionMax: 20 * time.Minute}
	for _, c := range []struct {
		asked, want time.Duration
		cut         bool
	}{
		{time.Hour, 20 * time.Minute, true},
		{20*time.Minute + time.Nanosecond, 20 * time.Minute, true},
		{20 * time.Minute, 20 * time.Minute, false},
		{10 * time.Minute, 10 * time.Minute, false},
	} {
		if got, cut := a.Length(c.asked); got != c.want || cut != c.cut {
			t.Fatalf("%v asked for gave %v, cut %v, want %v, cut %v", c.asked, got, cut, c.want, c.cut)
		}
	}
}

// UNIT-SETTINGS-21. Requirement: SET-01.
func TestUNITSETTINGS21_TheUsersFileCannotSetTheMaximum(t *testing.T) {
	wantError(t, "SESSION_MAX = 100h\n", "line 1", "SESSION_MAX", "/etc/gangway/settings")
}

// UNIT-SETTINGS-22. Requirement: SET-01, SET-02.
func TestUNITSETTINGS22_ThePlaceIsFixedAndTheFileIsOnlyRead(t *testing.T) {
	t.Setenv("GANGWAY_TEST_ADMIN_FOLDER", t.TempDir())
	if folder, owner := adminPlace(); folder != "/etc/gangway" || owner != 0 {
		t.Fatalf("the admin folder is %q, owned by %d, want /etc/gangway and root", folder, owner)
	}

	read := func(name string) string {
		code, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(code)
	}
	place := read("admin_place.go")
	if !strings.HasPrefix(place, "//go:build !gangwaytest\n") {
		t.Fatal("admin_place.go is not the file of every build but the test build")
	}
	if strings.Contains(place, "import") {
		t.Fatal("admin_place.go uses a package: the place must be fixed text")
	}
	if !strings.HasPrefix(read("admin_place_test_build.go"), "//go:build gangwaytest\n") {
		t.Fatal("admin_place_test_build.go can get into a build that is not for the tests")
	}
	admin := read("admin.go")
	if strings.Contains(admin, "Getenv") || strings.Contains(admin, "Environ") {
		t.Fatal("admin.go reads the surroundings")
	}
	for _, call := range []string{"os.Create", "os.OpenFile", "os.WriteFile", "os.Mkdir", "os.Remove", "os.Rename",
		"os.Chmod", "os.Chown", "os.Chtimes", "os.Truncate", "os.Symlink", "os.Link", "syscall.Open"} {
		if strings.Contains(admin, call) {
			t.Fatalf("admin.go uses %s: the admin folder must only be read", call)
		}
	}
	if strings.Count(admin, "os.Open(") != 1 || strings.Count(admin, "os.Lstat(") != 1 {
		t.Fatal("admin.go reaches the admin folder in a way this test does not follow")
	}
}
