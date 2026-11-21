package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// run is one real command. Its output is gathered until it ends.
type run struct {
	mu     sync.Mutex
	out    []byte
	exited chan int
	proc   Process
}

func (r *run) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.out)
}

func start(t *testing.T, shell, command string) (*run, error) {
	t.Helper()
	r := &run{exited: make(chan int, 1)}
	s := &OSStarter{
		Out: func(piece []byte) {
			r.mu.Lock()
			r.out = append(r.out, piece...)
			r.mu.Unlock()
		},
		Exit: func(code int) { r.exited <- code },
	}
	proc, err := s.Start(shell, command)
	r.proc = proc
	if err == nil {
		t.Cleanup(func() { proc.Kill() })
	}
	return r, err
}

func mustStart(t *testing.T, command string) *run {
	t.Helper()
	r, err := start(t, "/bin/sh", command)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// wait gives the exit code, or fails when the command does not end in time.
func (r *run) wait(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case code := <-r.exited:
		return code
	case <-time.After(within):
		t.Fatalf("the command did not end within %v. Output so far: %q", within, r.output())
		return 0
	}
}

func (r *run) stillRuns(t *testing.T, during time.Duration) {
	t.Helper()
	select {
	case code := <-r.exited:
		t.Fatalf("the command ended with %d, want it to still run", code)
	case <-time.After(during):
	}
}

// waitFor polls until ok, or fails.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

// alive tells whether a process with this pid runs. A process that ended
// but was not collected by its parent yet does not count.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}

// firstPid reads the pid that the command printed as its first line.
func firstPid(t *testing.T, r *run) int {
	t.Helper()
	waitFor(t, "the command prints a pid", func() bool { return strings.Contains(r.output(), "\n") })
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(r.output(), "\n", 2)[0]))
	if err != nil {
		t.Fatalf("no pid in %q", r.output())
	}
	return pid
}

// UNIT-RUNNER-21. Requirement: GATE-11.
func TestUNITRUNNER21_APipe(t *testing.T) {
	r := mustStart(t, `printf 'a\nb\nc\n' | tail -n 1`)
	if code := r.wait(t, 5*time.Second); code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	if got := r.output(); got != "c\n" {
		t.Fatalf("output %q, want %q", got, "c\n")
	}
}

// UNIT-RUNNER-22. Requirement: GATE-11.
func TestUNITRUNNER22_RedirectionAndTwoCommands(t *testing.T) {
	file := filepath.Join(t.TempDir(), "out.txt")
	r := mustStart(t, "echo hello > "+file+"; echo second")
	r.wait(t, 5*time.Second)
	if got, _ := os.ReadFile(file); string(got) != "hello\n" {
		t.Fatalf("the file holds %q, want %q", got, "hello\n")
	}
	if got := r.output(); got != "second\n" {
		t.Fatalf("output %q, want %q", got, "second\n")
	}
}

// UNIT-RUNNER-23. Requirement: GATE-08.
func TestUNITRUNNER23_ThePlainUser(t *testing.T) {
	r := mustStart(t, "id -u")
	r.wait(t, 5*time.Second)
	if got, want := strings.TrimSpace(r.output()), strconv.Itoa(os.Getuid()); got != want {
		t.Fatalf("the command ran as user %s, want %s", got, want)
	}
}

// UNIT-RUNNER-24. Requirement: GATE-01.
func TestUNITRUNNER24_TheExitCode(t *testing.T) {
	r := mustStart(t, "exit 7")
	if code := r.wait(t, 5*time.Second); code != 7 {
		t.Fatalf("exit code %d, want 7", code)
	}
}

// UNIT-RUNNER-25. Requirement: SHOW-02.
func TestUNITRUNNER25_BothOutputsInOrder(t *testing.T) {
	r := mustStart(t, "echo one; echo two >&2; echo three")
	r.wait(t, 5*time.Second)
	if got := r.output(); got != "one\ntwo\nthree\n" {
		t.Fatalf("output %q, want one, two, three", got)
	}
}

// UNIT-RUNNER-26. Requirement: GATE-17.
func TestUNITRUNNER26_NoKeyboard(t *testing.T) {
	r := mustStart(t, "cat")
	if code := r.wait(t, 5*time.Second); code != 0 {
		t.Fatalf("exit code %d, want 0", code)
	}
	if got := r.output(); got != "" {
		t.Fatalf("output %q, want none", got)
	}
}

// UNIT-RUNNER-27. Requirement: GATE-18.
func TestUNITRUNNER27_PlainSurroundings(t *testing.T) {
	t.Setenv("PAGER", "less")
	t.Setenv("TERM", "xterm-256color")
	r := mustStart(t, `echo "$PAGER $TERM"`)
	r.wait(t, 5*time.Second)
	if got := r.output(); got != "cat dumb\n" {
		t.Fatalf("output %q, want %q", got, "cat dumb\n")
	}
}

// UNIT-RUNNER-28. Requirement: LIM-11.
func TestUNITRUNNER28_AKillReachesEverythingTheCommandStarted(t *testing.T) {
	r := mustStart(t, "sleep 300 & echo $!; wait")
	pid := firstPid(t, r)
	if !alive(pid) {
		t.Fatal("the background program is not running")
	}
	r.proc.Stop()
	r.wait(t, 5*time.Second)
	waitFor(t, "the background program is gone", func() bool { return !alive(pid) })
}

// UNIT-RUNNER-29. Requirement: LIM-13.
func TestUNITRUNNER29_ForceKillsWhatDoesNotStop(t *testing.T) {
	r := mustStart(t, "trap '' TERM; echo ready; sleep 300")
	waitFor(t, "the command is ready", func() bool { return strings.Contains(r.output(), "ready") })
	r.proc.Stop()
	r.stillRuns(t, 300*time.Millisecond)
	r.proc.Kill()
	if code := r.wait(t, 5*time.Second); code != 128+int(syscall.SIGKILL) {
		t.Fatalf("exit code %d, want %d", code, 128+int(syscall.SIGKILL))
	}
}

// UNIT-RUNNER-30. Requirement: LIM-12.
func TestUNITRUNNER30_ABackgroundProgramOutlivesItsCommand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bg.log")
	for _, command := range []string{
		"sleep 300 > " + file + " 2>&1 & echo $!",
		"sleep 300 & echo $!", // the program holds the output pipe open
	} {
		r := mustStart(t, command)
		began := time.Now()
		if code := r.wait(t, 5*time.Second); code != 0 {
			t.Fatalf("%s: exit code %d, want 0", command, code)
		}
		if took := time.Since(began); took > 2*time.Second {
			t.Fatalf("%s: the exit took %v, want under two seconds", command, took)
		}
		pid := firstPid(t, r)
		defer syscall.Kill(pid, syscall.SIGKILL)
		if !alive(pid) {
			t.Fatalf("%s: the background program was killed", command)
		}
	}
}

// UNIT-RUNNER-31. Requirement: GATE-11.
func TestUNITRUNNER31_AShellThatIsNotThere(t *testing.T) {
	r, err := start(t, filepath.Join(t.TempDir(), "no-such-shell"), "echo hi")
	if err == nil {
		t.Fatal("no error for a shell that is not there")
	}
	select {
	case code := <-r.exited:
		t.Fatalf("an exit was reported: %d", code)
	case <-time.After(100 * time.Millisecond):
	}
}
