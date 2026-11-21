package runner

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// outputWait is how long the starter still reads the output after the
// shell has ended. A program left in the background can hold the pipe open.
const outputWait = 500 * time.Millisecond

// OSStarter starts real commands. Out and Exit are called from goroutines
// of its own, so the caller must hand them over to the one loop. For one
// command, every Out comes before its Exit.
type OSStarter struct {
	Out  func(piece []byte)
	Exit func(exitCode int)
}

type osProcess struct{ pid int }

// The command is the leader of its own process group, so its pid names the
// group, and a signal to -pid reaches every program it started.
func (p osProcess) Stop() { syscall.Kill(-p.pid, syscall.SIGTERM) }
func (p osProcess) Kill() { syscall.Kill(-p.pid, syscall.SIGKILL) }

type outWriter struct{ out func([]byte) }

func (w *outWriter) Write(b []byte) (int, error) {
	// The caller reuses b, so it is copied before it leaves this goroutine.
	w.out(append([]byte(nil), b...))
	return len(b), nil
}

// Start runs the command through the shell, with no keyboard.
func (s *OSStarter) Start(shell, command string) (Process, error) {
	cmd := exec.Command(shell, "-c", command)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "PAGER=cat", "TERM=dumb")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// One writer for both, so both go into one pipe and keep their order.
	// It must be a pointer: exec compares the two to see that they are one.
	out := &outWriter{s.Out}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = outputWait
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		cmd.Wait()
		s.Exit(exitCode(cmd.ProcessState))
	}()
	return osProcess{pid: cmd.Process.Pid}, nil
}

func exitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return state.ExitCode()
}
