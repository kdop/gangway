package rawterm

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openTerminal makes a real terminal for the test. What the test writes to
// keyboard is what a user would type; the program under test reads tty.
func openTerminal(t *testing.T) (keyboard, tty *os.File) {
	t.Helper()
	keyboard, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot make a terminal here: %v", err)
	}
	t.Cleanup(func() { keyboard.Close() })
	var unlock, number int32
	for _, call := range []struct {
		request uintptr
		arg     *int32
	}{{syscall.TIOCSPTLCK, &unlock}, {syscall.TIOCGPTN, &number}} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, keyboard.Fd(), call.request, uintptr(unsafe.Pointer(call.arg))); errno != 0 {
			t.Skipf("cannot make a terminal here: %v", errno)
		}
	}
	tty, err = os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot make a terminal here: %v", err)
	}
	t.Cleanup(func() { tty.Close() })
	return keyboard, tty
}

// readOne reads from the terminal, or fails when nothing comes in time.
func readOne(t *testing.T, tty *os.File) []byte {
	t.Helper()
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := tty.Read(buf)
		got <- buf[:n]
	}()
	select {
	case b := <-got:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("nothing could be read from the terminal")
		return nil
	}
}

func rawTerminal(t *testing.T) (keyboard, tty *os.File) {
	t.Helper()
	keyboard, tty = openTerminal(t)
	if _, err := MakeRaw(int(tty.Fd())); err != nil {
		t.Fatal(err)
	}
	return keyboard, tty
}

// UNIT-TERMINAL-24. Requirement: GATE-03.
func TestUNITTERMINAL24_AKeyComesWithNoEnter(t *testing.T) {
	keyboard, tty := rawTerminal(t)
	keyboard.Write([]byte("y"))
	if got := readOne(t, tty); string(got) != "y" {
		t.Fatalf("read %q, want %q", got, "y")
	}
}

// UNIT-TERMINAL-25. Requirement: LIM-09.
func TestUNITTERMINAL25_CtrlCComesAsAKey(t *testing.T) {
	keyboard, tty := rawTerminal(t)
	keyboard.Write([]byte{3})
	if got := readOne(t, tty); len(got) != 1 || got[0] != 3 {
		t.Fatalf("read %q, want code 3", got)
	}
}

// UNIT-TERMINAL-26. Requirement: HOST-02.
func TestUNITTERMINAL26_IsTerminal(t *testing.T) {
	_, tty := openTerminal(t)
	if !IsTerminal(int(tty.Fd())) {
		t.Fatal("a real terminal is said not to be one")
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "plain"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if IsTerminal(int(file.Fd())) {
		t.Fatal("a plain file is said to be a terminal")
	}
}

// UNIT-TERMINAL-27. Requirement: HOST-07.
func TestUNITTERMINAL27_RestorePutsTheTerminalBack(t *testing.T) {
	_, tty := openTerminal(t)
	fd := int(tty.Fd())
	before, err := State(fd)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := MakeRaw(fd)
	if err != nil {
		t.Fatal(err)
	}
	during, _ := State(fd)
	if during == before {
		t.Fatal("raw mode changed nothing")
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	after, _ := State(fd)
	if after != before {
		t.Fatalf("the settings after are %+v, want %+v", after, before)
	}
}
