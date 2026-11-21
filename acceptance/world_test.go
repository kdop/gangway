// Package acceptance holds the acceptance tests of requirements.md. Each
// test starts the real controller and the real cli-tool, calls the API as
// the AI would, and presses keys as the user would.
package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const (
	question = "Run it? y/n "

	// promptMinShow is the PROMPT_MIN_SHOW the tests run with, unless a
	// test sets its own.
	promptMinShow = 100 * time.Millisecond

	// soon is how long a test waits for something that must happen.
	soon = 10 * time.Second
	// quiet is how long a test watches to see that nothing happens.
	quiet = 400 * time.Millisecond
)

var controllerBin, cliBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gangway-acceptance")
	if err != nil {
		panic(err)
	}
	controllerBin = filepath.Join(dir, "controller")
	cliBin = filepath.Join(dir, "gangway")
	build := func(folder, out, pkg string) {
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Dir = filepath.Join("..", folder)
		if b, err := cmd.CombinedOutput(); err != nil {
			panic(fmt.Sprintf("build of %s: %v\n%s", folder, err, b))
		}
	}
	build("controller", controllerBin, ".")
	build("cli", cliBin, "./cmd/gangway")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// screen collects what the cli-tool writes to its terminal.
type screen struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// raw gives every byte that reached the terminal.
func (s *screen) raw() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// text gives the screen as lines that end in a plain line break.
func (s *screen) text() string { return strings.ReplaceAll(s.raw(), "\r\n", "\n") }

// world is one controller and one cli-tool with a live session.
type world struct {
	t       *testing.T
	dir     string // an empty folder for the files of the test
	home    string // the home folder both programs run with
	listen  string
	address string
	key     string
	screen  *screen
	keys    *os.File
	ctl     **exec.Cmd // shared by every cli-tool on this controller
	cli     *exec.Cmd
	cliDone chan struct{}
}

// newWorld starts both programs and waits for the paste line. Each of
// settings is one line of the cli-tool's settings file.
func newWorld(t *testing.T, settings ...string) *world {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()

	w := &world{t: t, home: t.TempDir(), listen: listen, address: "http://" + listen, ctl: new(*exec.Cmd)}
	w.startController()
	t.Cleanup(w.stopController)
	w.startCli(settings)
	return w
}

// join starts one more cli-tool on the same controller.
func (w *world) join(settings ...string) *world {
	w.t.Helper()
	more := &world{t: w.t, home: w.home, listen: w.listen, address: w.address, ctl: w.ctl}
	more.startCli(settings)
	return more
}

// startController starts the controller and waits until it listens.
func (w *world) startController() {
	w.t.Helper()
	ctl := exec.Command(controllerBin, "-listen", w.listen)
	ctl.Dir = w.home
	ctl.Env = append(os.Environ(), "HOME="+w.home)
	if err := ctl.Start(); err != nil {
		w.t.Fatal(err)
	}
	*w.ctl = ctl
	w.until("the controller listens", func() bool {
		c, err := net.Dial("tcp", w.listen)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
}

// stopController kills the controller, as a crash would.
func (w *world) stopController() {
	(*w.ctl).Process.Kill()
	(*w.ctl).Wait()
}

// startCli starts a cli-tool on a terminal of its own and waits for the
// paste line.
func (w *world) startCli(settings []string) {
	w.t.Helper()
	t := w.t
	w.dir, w.screen, w.cliDone = t.TempDir(), &screen{}, make(chan struct{})

	config, err := os.MkdirTemp(w.home, "config")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{"CONTROLLER_ADDRESS = " + w.address}
	if !hasSetting(settings, "PROMPT_MIN_SHOW") {
		lines = append(lines, "PROMPT_MIN_SHOW = "+promptMinShow.String())
	}
	lines = append(lines, settings...)
	if err := os.MkdirAll(filepath.Join(config, "gangway"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(config, "gangway", "settings")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	master, slave := openTerminal(t)
	w.keys = master
	w.cli = exec.Command(cliBin)
	w.cli.Dir = w.home
	w.cli.Stdin, w.cli.Stdout, w.cli.Stderr = slave, slave, slave
	w.cli.Env = append(os.Environ(), "HOME="+w.home, "XDG_CONFIG_HOME="+config)
	if err := w.cli.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	go io.Copy(w.screen, master)
	done := w.cliDone
	go func() { w.cli.Wait(); close(done) }()
	t.Cleanup(func() {
		w.cli.Process.Kill()
		<-done
		master.Close()
		if t.Failed() {
			t.Logf("the screen:\n%s", strings.ReplaceAll(w.screen.text(), w.key, "gw_<key>"))
		}
	})

	w.until("the paste line shows", func() bool {
		for _, l := range strings.Split(w.screen.text(), "\n") {
			if f := strings.Fields(l); len(f) == 3 && f[0] == "/gangway" {
				w.key = f[2]
				return true
			}
		}
		return false
	})
}

// ended waits until the cli-tool has ended.
func (w *world) ended() {
	w.t.Helper()
	select {
	case <-w.cliDone:
	case <-time.After(soon):
		w.t.Fatal("the cli-tool did not end")
	}
}

func hasSetting(settings []string, name string) bool {
	for _, s := range settings {
		if strings.HasPrefix(s, name) {
			return true
		}
	}
	return false
}

// openTerminal makes a terminal for the cli-tool: the test holds one end,
// the cli-tool gets the other.
func openTerminal(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var unlock, n int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Fatal(e)
	}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Fatal(e)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return master, slave
}

// until waits for ok to turn true, and fails the test if it does not.
func (w *world) until(what string, ok func() bool) {
	w.t.Helper()
	for end := time.Now().Add(soon); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	w.t.Fatalf("waited in vain: %s", what)
}

// stays watches for a short time and fails the test if ok turns false.
func (w *world) stays(what string, ok func() bool) {
	w.t.Helper()
	for end := time.Now().Add(quiet); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if !ok() {
			w.t.Fatalf("did not stay true: %s", what)
		}
	}
}

// answer is what the API gave back.
type answer struct {
	Status        int
	Body          string
	State         string `json:"state"`
	Reason        string `json:"reason"`
	How           string `json:"how"`
	ExitCode      *int   `json:"exit_code"`
	Output        string `json:"output"`
	ID            string `json:"id"`
	BytesReplaced bool   `json:"bytes_replaced"`
}

// call makes one API call with the session key.
func (w *world) call(ctx context.Context, method, path, body string) (answer, error) {
	return w.callWith(ctx, w.key, method, path, body)
}

// callWith makes one API call with this key. An empty key sends none.
func (w *world) callWith(ctx context.Context, key, method, path, body string) (answer, error) {
	req, err := http.NewRequestWithContext(ctx, method, w.address+path, strings.NewReader(body))
	if err != nil {
		return answer{}, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	a := answer{Status: resp.StatusCode, Body: string(b)}
	json.Unmarshal(b, &a)
	return a, nil
}

// asked is a request the AI sent and still waits on.
type asked struct {
	w      *world
	done   chan answer
	hangUp context.CancelFunc
}

// ask sends a command as the AI would. The call stays open.
func (w *world) ask(command string) *asked {
	body, _ := json.Marshal(map[string]string{"kind": "command", "command": command})
	ctx, cancel := context.WithCancel(context.Background())
	a := &asked{w: w, done: make(chan answer, 1), hangUp: cancel}
	w.t.Cleanup(cancel)
	go func() {
		got, err := w.call(ctx, "POST", "/v1/requests", string(body))
		if err != nil {
			got = answer{Body: "the call failed: " + err.Error()}
		}
		a.done <- got
	}()
	return a
}

// answer waits for the answer of the API.
func (a *asked) answer() answer {
	a.w.t.Helper()
	select {
	case got := <-a.done:
		return got
	case <-time.After(soon):
		a.w.t.Fatal("the AI got no answer")
		return answer{}
	}
}

// noAnswerYet fails the test if the AI gets an answer in a short time.
func (a *asked) noAnswerYet() {
	a.w.t.Helper()
	select {
	case got := <-a.done:
		a.w.t.Fatalf("the AI got an answer too early: %d %s", got.Status, got.Body)
	case <-time.After(quiet):
	}
}

// ran fails the test unless the answer says the command ran and ended by
// itself with this exit code.
func (got answer) ran(t *testing.T, exitCode int) {
	t.Helper()
	if got.Status != 200 || got.State != "ended" || got.How != "exit" || got.ExitCode == nil || *got.ExitCode != exitCode {
		t.Fatalf("want a command that ended with exit code %d, got %d %s", exitCode, got.Status, got.Body)
	}
}

// refused fails the test unless the answer is "refused" with this reason.
func (got answer) refused(t *testing.T, reason string) {
	t.Helper()
	if got.Status != 200 || got.State != "refused" || got.Reason != reason {
		t.Fatalf("want refused with reason %q, got %d %s", reason, got.Status, got.Body)
	}
}

// prompts counts the prompts shown so far: the lines that start with the
// question.
func (w *world) prompts() int {
	return strings.Count("\n"+w.screen.text(), "\n"+question)
}

// prompt waits until n prompts were shown, and until a key counts.
func (w *world) prompt(n int) {
	w.t.Helper()
	w.promptShown(n)
	time.Sleep(2 * promptMinShow)
}

// promptShown waits until n prompts were shown. A key may not count yet.
func (w *world) promptShown(n int) {
	w.t.Helper()
	w.until(fmt.Sprintf("prompt %d shows", n), func() bool { return w.prompts() >= n })
	if got := w.prompts(); got != n {
		w.t.Fatalf("want %d prompts on the screen, got %d", n, got)
	}
}

// promptWaits fails the test if another prompt shows in a short time.
func (w *world) promptWaits(n int) {
	w.t.Helper()
	w.stays(fmt.Sprintf("the screen holds %d prompts", n), func() bool { return w.prompts() == n })
}

// waiting waits until n requests of the session wait for their answer.
func (w *world) waiting(n int) {
	w.t.Helper()
	w.until(fmt.Sprintf("%d requests wait", n), func() bool {
		a, err := w.call(context.Background(), "GET", "/v1/session", "")
		if err != nil {
			return false
		}
		var body struct {
			Requests struct {
				Waiting int `json:"waiting"`
			} `json:"requests"`
		}
		json.Unmarshal([]byte(a.Body), &body)
		return body.Requests.Waiting == n
	})
}

// press types keys at the terminal of the cli-tool.
func (w *world) press(keys string) {
	w.t.Helper()
	if _, err := w.keys.Write([]byte(keys)); err != nil {
		w.t.Fatal(err)
	}
}

// file gives a path in the folder of the test.
func (w *world) file(name string) string { return filepath.Join(w.dir, name) }

// exists tells whether a file of the test is there.
func (w *world) exists(name string) bool {
	_, err := os.Stat(w.file(name))
	return err == nil
}

// notRun fails the test if the mark file of a command shows up.
func (w *world) notRun(name string) {
	w.t.Helper()
	w.stays("the command has not run", func() bool { return !w.exists(name) })
}

// shows waits until the screen holds the text.
func (w *world) shows(text string) {
	w.t.Helper()
	w.until(fmt.Sprintf("the screen shows %q", text), func() bool { return strings.Contains(w.screen.text(), text) })
}

// skip skips a test that this machine cannot run. On GitHub it fails the
// test, so that every test runs there.
func skip(t *testing.T, why string) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatalf("must not be skipped on GitHub: %s", why)
	}
	t.Skip(why)
}
