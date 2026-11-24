package acceptance

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// The cli-tool of these tests looks for the admin file in a folder of the
// test, in the place of /etc/gangway. See TestMain.

// endsAtTheMaximum fails the test unless the session ends when max has
// passed, with no question to extend.
func (w *world) endsAtTheMaximum(max time.Duration) {
	w.t.Helper()
	w.live()
	w.dead()
	if lasted := time.Since(w.started); lasted < max-margin || lasted > max+3*time.Second {
		w.t.Fatalf("the session ended after %s, want at the maximum of %s", lasted.Round(time.Millisecond), max)
	}
	if got := w.questions(); got != 0 {
		w.t.Fatalf("%d questions to extend were shown, want none", got)
	}
}

func TestSET01_1_TheUsersSettingsCannotPassTheMaximum(t *testing.T) {
	w := newWorldUnder(t, "SESSION_MAX = 3s", "", "SESSION_LENGTH = 9s")
	w.endsAtTheMaximum(3 * time.Second)
}

func TestSET01_2_AStartOptionCannotPassTheMaximum(t *testing.T) {
	w := newWorldUnder(t, "SESSION_MAX = 3s", "9s")
	w.endsAtTheMaximum(3 * time.Second)
}

func TestSET01_3_ALengthBelowTheMaximumIsKept(t *testing.T) {
	const length = 3 * time.Second
	w := newWorldUnder(t, "SESSION_MAX = 20m", "3s")
	w.noQuestionBefore(w.started.Add(length-margin), 0)
	w.question(1)
	if late := time.Since(w.started) - length; late > 3*time.Second {
		t.Fatalf("the question came %s after the length asked for had passed", late)
	}
	w.shows(extendQuestion("3 s"))
	if !w.sessionTime().TimeUp {
		t.Fatal("the API does not say that the time is up")
	}
}

// watch tells the kernel to note every change to a path, and every time
// it was open for writing and then closed.
type watch struct {
	t  *testing.T
	fd int
}

const changes = syscall.IN_MODIFY | syscall.IN_ATTRIB | syscall.IN_CLOSE_WRITE | syscall.IN_CREATE |
	syscall.IN_DELETE | syscall.IN_DELETE_SELF | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_MOVE_SELF

func watchChanges(t *testing.T, paths ...string) *watch {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	for _, path := range paths {
		if _, err := syscall.InotifyAddWatch(fd, path, changes); err != nil {
			t.Fatal(err)
		}
	}
	return &watch{t, fd}
}

// seen counts the changes noted so far.
func (w *watch) seen() int {
	w.t.Helper()
	count := 0
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(w.fd, buf)
		if err == syscall.EAGAIN || n <= 0 {
			return count
		}
		if err != nil {
			w.t.Fatal(err)
		}
		for at := 0; at+syscall.SizeofInotifyEvent <= n; {
			event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[at]))
			count++
			at += syscall.SizeofInotifyEvent + int(event.Len)
		}
	}
}

func TestSET02_1_TheAdminFolderIsOnlyRead(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, "")
	w.admin = "SESSION_MAX = 45m"
	w.writeAdmin()
	// The file could be written by this user: nothing but the cli-tool's
	// own code holds it back.
	before, err := os.Stat(w.adminFile())
	if err != nil {
		t.Fatal(err)
	}
	folderBefore, err := os.Stat(w.adminFolder())
	if err != nil {
		t.Fatal(err)
	}
	watched := watchChanges(t, w.adminFolder(), w.adminFile())

	// The watch itself works: a file opened for writing and closed is
	// noted, with nothing written.
	probe := filepath.Join(t.TempDir(), "probe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	probed := watchChanges(t, probe)
	f, err := os.OpenFile(probe, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if probed.seen() == 0 {
		t.Fatal("the watch does not note a file that was opened for writing")
	}

	w.startTailscaleController()
	w.cliLogin(tailscaleKey).loggedIn(tailscaleKey, cliReady)
	w.startCli(nil)
	// The file was read: the screen names its maximum.
	w.shows("up to the maximum of 45 min\n")
	w.run("echo a whole session").ran(t, 0)
	w.endByCtrlC()

	if n := watched.seen(); n != 0 {
		t.Fatalf("the admin folder or its file was changed or opened for writing: %d times", n)
	}
	after, err := os.Stat(w.adminFile())
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		t.Fatal("the admin file has changed")
	}
	if text, err := os.ReadFile(w.adminFile()); err != nil || string(text) != "SESSION_MAX = 45m\n" {
		t.Fatalf("the admin file now holds %q, %v", text, err)
	}
	folderAfter, err := os.Stat(w.adminFolder())
	if err != nil {
		t.Fatal(err)
	}
	if !folderAfter.ModTime().Equal(folderBefore.ModTime()) || folderAfter.Mode() != folderBefore.Mode() {
		t.Fatal("the admin folder has changed")
	}
	entries, err := os.ReadDir(w.adminFolder())
	if err != nil || len(entries) != 1 || entries[0].Name() != "settings" {
		t.Fatalf("the admin folder now holds %v, %v", entries, err)
	}
}

func TestSET03_1_WithNoAdminFolderNoMaximumCuts(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2h")
	if _, err := os.Stat(w.adminFolder()); !os.IsNotExist(err) {
		t.Fatalf("the test has an admin folder: %v", err)
	}
	w.shows("\n  session   2 h, then you are asked to extend\n")
	if got := w.sessionTime(); got.TimeUp || got.SecondsLeft > 7200 || got.SecondsLeft < 7200-30 {
		t.Fatalf("the API says %+v, want a session with two hours left", got)
	}
	if strings.Contains(w.screen.text(), "maximum") {
		t.Fatal("the screen names a maximum")
	}
	w.run("echo no maximum").ran(t, 0)
}

// cutTo20 fails the test unless the session started with 20 minutes and
// the terminal says that one hour was cut.
func (w *world) cutTo20() {
	w.t.Helper()
	w.shows("\n  session   20 min, the maximum: then it ends\n" +
		"            1 h is above the maximum: it was cut to 20 min\n" +
		"            the maximum is set in " + w.adminFile() + "\n")
	if got := w.sessionTime(); got.TimeUp || got.SecondsLeft > 1200 || got.SecondsLeft < 1200-30 {
		w.t.Fatalf("the API says %+v, want a session with 20 minutes left", got)
	}
}

func TestSET04_1_AStartOptionAboveTheMaximumIsCutAndSaid(t *testing.T) {
	newWorldUnder(t, "SESSION_MAX = 20m", "60m").cutTo20()
}

func TestSET04_2_AUsersSettingAboveTheMaximumIsCutAndSaid(t *testing.T) {
	newWorldUnder(t, "SESSION_MAX = 20m", "", "SESSION_LENGTH = 60m").cutTo20()
}

func TestSET04_3_ALengthBelowTheMaximumIsNotCut(t *testing.T) {
	w := newWorldUnder(t, "SESSION_MAX = 20m", "10m")
	w.shows("\n  session   10 min, then you are asked to extend, up to the maximum of 20 min\n")
	if got := w.sessionTime(); got.TimeUp || got.SecondsLeft > 600 || got.SecondsLeft < 600-30 {
		t.Fatalf("the API says %+v, want a session with ten minutes left", got)
	}
	if strings.Contains(w.screen.text(), "above the maximum") || strings.Contains(w.screen.text(), "cut") {
		t.Fatal("the screen says that the length was cut")
	}
}

// refusedAdminFile starts a cli-tool on a host whose admin file is made
// by prepare, and fails the test unless the cli-tool says each of words
// and ends with no session.
func refusedAdminFile(t *testing.T, text string, prepare func(w *world), words ...string) {
	t.Helper()
	beside(t)
	ln := newWorld(t) // a controller, and a first cli-tool with no admin folder
	w := &world{t: t, home: t.TempDir(), listen: ln.listen, address: ln.address, ctl: ln.ctl, admin: text, noPasteLine: true}
	w.writeAdmin()
	if prepare != nil {
		prepare(w)
	}
	w.startCli(nil)
	w.ended()
	if code := w.cli.ProcessState.ExitCode(); code == 0 {
		t.Fatal("the cli-tool ended with exit code 0")
	}
	for _, word := range append(words, w.adminFile()) {
		w.shows(word)
	}
	if strings.Contains(w.screen.text(), "/gangway ") {
		t.Fatal("a paste line was shown")
	}
	// The controller was not asked for a session: the first one is still
	// the only one, and live.
	ln.live()
}

func TestSET05_1_AMistakeInTheAdminFileStopsTheStart(t *testing.T) {
	refusedAdminFile(t, "SESSION_MAX = 20m\nSESSION_LENGTH = 10m", nil, "line 2", "SESSION_LENGTH")
}

func TestSET05_2_AnAdminFileThatOthersCanWriteStopsTheStart(t *testing.T) {
	refusedAdminFile(t, "SESSION_MAX = 20m", func(w *world) {
		if err := os.Chmod(w.adminFile(), 0o666); err != nil {
			t.Fatal(err)
		}
	}, "writable by")
}

func TestSET05_3_AnAdminFileThatCannotBeReadStopsTheStart(t *testing.T) {
	if os.Geteuid() == 0 {
		skip(t, "root can read every file")
	}
	refusedAdminFile(t, "SESSION_MAX = 20m", func(w *world) {
		if err := os.Chmod(w.adminFile(), 0); err != nil {
			t.Fatal(err)
		}
	}, "cannot read")
}
