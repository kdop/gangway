package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var background = context.Background()

// status makes one call with a key and gives the HTTP status.
func (w *world) status(key, method, path, body string) int {
	w.t.Helper()
	got, err := w.callWith(background, key, method, path, body)
	if err != nil {
		w.t.Fatal(err)
	}
	return got.Status
}

// keyRefused fails the test unless the key is turned away, for a hello
// and for a request, and no prompt shows.
func (w *world) keyRefused(key string) {
	w.t.Helper()
	before := w.prompts()
	if s := w.status(key, "POST", "/v1/hello", ""); s != 401 {
		w.t.Fatalf("want 401 for hello, got %d", s)
	}
	request := `{"kind":"command","command":"touch ` + w.file("ran") + `"}`
	if s := w.status(key, "POST", "/v1/requests", request); s != 401 {
		w.t.Fatalf("want 401 for a request, got %d", s)
	}
	w.promptWaits(before)
	w.notRun("ran")
}

// endByCtrlC ends the session with one Ctrl+C and waits for the cli-tool
// to end.
func (w *world) endByCtrlC() {
	w.t.Helper()
	w.press("\x03")
	w.ended()
}

// runsLong asks for a command that runs for long, approves it, and gives
// its process ID.
func (w *world) runsLong() (*asked, int) {
	w.t.Helper()
	n := w.prompts()
	a := w.ask("echo $$ > " + w.file("pid") + "; exec sleep 300")
	w.prompt(n + 1)
	w.press("y")
	var pid int
	w.until("the command runs", func() bool {
		b, _ := os.ReadFile(w.file("pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return pid > 0
	})
	w.t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	return a, pid
}

// gone waits until the program with this process ID is gone.
func (w *world) gone(pid int) {
	w.t.Helper()
	w.until("the command is gone", func() bool { return syscall.Kill(pid, 0) != nil })
}

func TestSES01_1_AMadeUpKeyIsRefused(t *testing.T) {
	w := newWorld(t)
	w.keyRefused("gw_" + strings.Repeat("0", 64))
}

func TestSES01_2_TheKeyOfAnEndedSessionIsRefused(t *testing.T) {
	w := newWorld(t)
	w.endByCtrlC()
	w.keyRefused(w.key)
}

func TestSES02_1_CtrlCWithNothingInFrontEndsTheSession(t *testing.T) {
	w := newWorld(t)
	w.live()
	w.press("\x03")
	w.ended()
	w.shows("\n  reason    Ctrl+C\n")
	w.keyRefused(w.key)
}

func TestSES03_1_AnEndedSessionTakesNoRequest(t *testing.T) {
	w := newWorld(t)
	w.endByCtrlC()
	second := w.join()
	w.keyRefused(w.key)
	second.promptWaits(0)
}

func TestSES03_2_ANewStartMakesANewKey(t *testing.T) {
	w := newWorld(t)
	w.endByCtrlC()
	second := w.join()
	if second.key == w.key {
		t.Fatal("the new session has the old key")
	}
	second.live()
	w.keyRefused(w.key)
}

func TestSES03_3_OldRequestsDoNotComeBack(t *testing.T) {
	w := newWorld(t, "DOUBLE_CTRL_C_WINDOW = 2s")
	w.ask("touch " + w.file("a"))
	w.prompt(1)
	w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("\x03\x03")
	w.ended()
	second := w.join()
	second.promptWaits(0)
	w.notRun("a")
	w.notRun("b")
}

func TestSES05_1_TwoCtrlCStopTheCommand(t *testing.T) {
	w := newWorld(t)
	_, pid := w.runsLong()
	w.press("\x03\x03")
	w.ended()
	w.gone(pid)
}

// The MVP has no reconnect: a lost connection ends the session at once.
func TestSES05_2_ALostConnectionStopsTheCommand(t *testing.T) {
	w := newWorld(t)
	_, pid := w.runsLong()
	w.stopController()
	w.ended()
	w.gone(pid)
	w.shows("\n  reason    Connection lost\n")
}

// filesHold tells which files under the home folder of both programs hold
// one of the words.
func (w *world) filesHold(words ...string) []string {
	w.t.Helper()
	var found []string
	err := filepath.WalkDir(w.home, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range words {
			if bytes.Contains(b, []byte(word)) {
				found = append(found, path)
			}
		}
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return found
}

// secrets gives the key in the forms a file could hold it.
func (w *world) secrets() []string {
	hash := sha256.Sum256([]byte(w.key))
	return []string{w.key, strings.TrimPrefix(w.key, "gw_"), hex.EncodeToString(hash[:])}
}

func TestSES06_1_NoFileHoldsALiveSession(t *testing.T) {
	w := newWorld(t)
	w.run("echo one").ran(t, 0)
	w.run("echo two").ran(t, 0)
	if found := w.filesHold(w.secrets()...); len(found) > 0 {
		t.Fatalf("the key or its hash is in: %v", found)
	}
}

func TestSES06_2_NoFileHoldsAnEndedSession(t *testing.T) {
	w := newWorld(t)
	w.run("echo one").ran(t, 0)
	w.endByCtrlC()
	if found := w.filesHold(w.secrets()...); len(found) > 0 {
		t.Fatalf("the key or its hash is in: %v", found)
	}
}

// restarted gives two cli-tools whose controller was stopped and started
// again.
func restarted(t *testing.T) (*world, *world) {
	w := newWorld(t)
	second := w.join()
	w.live()
	second.live()
	w.stopController()
	w.ended()
	second.ended()
	w.startController()
	return w, second
}

func TestSES07_1_ARestartedControllerHasNoSession(t *testing.T) {
	w, second := restarted(t)
	w.keyRefused(w.key)
	w.keyRefused(second.key)
}

func TestSES07_2_EachCliToolSaysTheControllerWentAway(t *testing.T) {
	w, second := restarted(t)
	for _, each := range []*world{w, second} {
		each.shows("\n  reason    Connection lost\n")
		each.screenEnds("\n  log       " + each.logFile() + "\n")
	}
}

var keyForm = regexp.MustCompile(`^gw_[0-9a-f]{64}$`)

// 64 letters of 4 bits each are the 256 bits of SESSION_KEY_BITS.
func TestKEY01_1_TheKeyHoldsAllItsBits(t *testing.T) {
	w := newWorld(t)
	if !keyForm.MatchString(w.key) {
		t.Fatalf("the key has not the form gw_ and 64 letters of 0-9 and a-f: %d letters", len(w.key))
	}
}

func TestKEY01_2_AHundredStartsGiveAHundredKeys(t *testing.T) {
	w := newWorld(t)
	seen := map[string]bool{w.key: true}
	for i := 1; i < 100; i++ {
		next := w.join()
		if seen[next.key] {
			t.Fatalf("start %d gave a key that was seen before", i+1)
		}
		seen[next.key] = true
		next.cli.Process.Kill()
	}
}

func TestKEY02_1_NoKeyIsRefused(t *testing.T) {
	w := newWorld(t)
	w.keyRefused("")
}

func TestKEY02_2_AWrongKeyIsRefused(t *testing.T) {
	w := newWorld(t)
	other := w.join()
	other.endByCtrlC()
	w.keyRefused(other.key)
	w.keyRefused("wrong")
}

func TestKEY02_3_AKeyOneLetterOffIsRefused(t *testing.T) {
	w := newWorld(t)
	w.keyRefused(w.key[:len(w.key)-1])
	w.keyRefused(w.key + "0")
	w.live()
}

func TestKEY03_1_TheKeyDiesWithTheSession(t *testing.T) {
	w := newWorld(t)
	w.live()
	w.press("\x03")
	w.shows("\n  ── session over ─")
	w.screenEnds("\n  log       " + w.logFile() + "\n")
	if s := w.status(w.key, "POST", "/v1/hello", ""); s != 401 {
		t.Fatalf("want 401 right after the end, got %d", s)
	}
}

func TestKEY04_1_ThePasteLineHasItsForm(t *testing.T) {
	w := newWorld(t)
	form := regexp.MustCompile(`(?m)^/gangway http://127\.0\.0\.1:\d+ gw_[0-9a-f]{64}$`)
	if !form.MatchString(w.screen.text()) {
		t.Fatal("no line of the form /gangway <address> gw_<key>")
	}
}

func TestKEY04_2_ThePasteLineWorks(t *testing.T) {
	w := newWorld(t)
	var address, key string
	for _, l := range strings.Split(w.screen.text(), "\n") {
		if f := strings.Fields(l); len(f) == 3 && f[0] == "/gangway" {
			address, key = f[1], f[2]
		}
	}
	paste := &world{t: t, address: address, key: key}
	paste.live()
}

// asRoot gives the words that start a program as root, or skips the test
// when this machine has no way to do that without a password.
func asRoot(t *testing.T) []string {
	for _, how := range [][]string{{"sudo", "-n"}, {"unshare", "--user", "--map-root-user"}} {
		if exec.Command(how[0], append(how[1:], "true")...).Run() == nil {
			return how
		}
	}
	skip(t, "no way to become root here without a password")
	return nil
}

// refusesToStart starts the cli-tool in a way it must refuse, and gives
// what it printed. connected tells whether anything reached the controller.
func refusesToStart(t *testing.T, prefix []string, onTerminal bool) (printed string, connected bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan bool, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
		got <- err == nil
	}()

	config := t.TempDir()
	os.MkdirAll(filepath.Join(config, "gangway"), 0o755)
	os.Chmod(config, 0o755)
	settings := "# the controller is at " + ln.Addr().String() + "\n"
	if err := os.WriteFile(filepath.Join(config, "gangway", "settings"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	words := append(append([]string{}, prefix...), "env", "XDG_CONFIG_HOME="+config, cliBin, "--local", "http://"+ln.Addr().String())
	cmd := exec.Command(words[0], words[1:]...)
	var out bytes.Buffer
	scr := &screen{}
	if onTerminal {
		master, slave := openTerminal(t)
		defer master.Close()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := master.Read(buf)
				scr.Write(buf[:n])
				if err != nil {
					return
				}
			}
		}()
		defer slave.Close()
	} else {
		cmd.Stdout, cmd.Stderr = &out, &out
	}
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the cli-tool ended with no error")
		}
	case <-time.After(soon):
		cmd.Process.Kill()
		t.Fatal("the cli-tool did not exit")
	}
	select {
	case connected = <-got:
	case <-time.After(quiet):
	}
	if onTerminal {
		time.Sleep(100 * time.Millisecond)
		return scr.text(), connected
	}
	return out.String(), connected
}

func rootRefused(t *testing.T, prefix []string) {
	t.Helper()
	printed, connected := refusesToStart(t, prefix, true)
	if !strings.Contains(printed, "does not run as root") {
		t.Fatalf("the reason is not printed: %q", printed)
	}
	if connected {
		t.Fatal("the cli-tool made a connection")
	}
}

func TestHOST01_1_RefusesToStartAsRoot(t *testing.T) {
	rootRefused(t, asRoot(t))
}

func TestHOST01_2_RefusesToStartWithSudo(t *testing.T) {
	if exec.Command("sudo", "-n", "true").Run() != nil {
		skip(t, "sudo asks for a password here")
	}
	rootRefused(t, []string{"sudo", "-n"})
}

func TestHOST02_1_RefusesToStartWithNoTerminalForInput(t *testing.T) {
	printed, connected := refusesToStart(t, nil, false)
	if !strings.Contains(printed, "needs a terminal") || connected {
		t.Fatalf("want the reason and no connection, got %q, connected: %v", printed, connected)
	}
}

// A program started in the background of a shell has no terminal to read
// from: its input, output and errors go elsewhere.
func TestHOST02_2_RefusesToStartInTheBackground(t *testing.T) {
	printed, connected := refusesToStart(t, []string{"setsid"}, false)
	if !strings.Contains(printed, "needs a terminal") || connected {
		t.Fatalf("want the reason and no connection, got %q, connected: %v", printed, connected)
	}
}

// terminalState reads how the terminal of the cli-tool is set.
func (w *world) terminalState() syscall.Termios {
	w.t.Helper()
	var state syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, w.keys.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state))); e != 0 {
		w.t.Fatal(e)
	}
	return state
}

// A terminal that shows typed letters and waits for Enter has ECHO and
// ICANON set. The cli-tool switches both off while it runs.
func (w *world) terminalAsFound() {
	w.t.Helper()
	state := w.terminalState()
	if state.Lflag&syscall.ECHO == 0 || state.Lflag&syscall.ICANON == 0 || state.Lflag&syscall.ISIG == 0 {
		w.t.Fatal("the terminal was not put back: typed letters do not show, or Enter does not work")
	}
}

func (w *world) terminalTaken() {
	w.t.Helper()
	if state := w.terminalState(); state.Lflag&syscall.ECHO != 0 || state.Lflag&syscall.ICANON != 0 {
		w.t.Fatal("the test is of no use: the cli-tool has not changed the terminal")
	}
}

func TestHOST07_1_TheTerminalIsLeftAsFoundAfterTwoCtrlC(t *testing.T) {
	w := newWorld(t)
	w.terminalTaken()
	w.press("\x03\x03")
	w.ended()
	w.terminalAsFound()
}

func TestHOST07_2_TheTerminalIsLeftAsFoundWhenEndedFromOutside(t *testing.T) {
	w := newWorld(t)
	_, pid := w.runsLong()
	w.terminalTaken()
	w.cli.Process.Signal(syscall.SIGTERM)
	w.ended()
	w.terminalAsFound()
	w.gone(pid)
}
