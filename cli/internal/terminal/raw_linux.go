package terminal

import (
	"syscall"
	"unsafe"
)

func getTermios(fd int) (syscall.Termios, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return t, errno
	}
	return t, nil
}

func setTermios(fd int, t syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return errno
	}
	return nil
}

// IsTerminal tells whether the file is a terminal.
func IsTerminal(fd int) bool {
	_, err := getTermios(fd)
	return err == nil
}

// MakeRaw sets the terminal so that every key comes at once, with no
// Enter, is not shown, and Ctrl+C comes as a key and not as a signal.
// Output is left as it was, so a line break still starts a new line.
// restore puts the terminal back as it was found. It must be called before
// the program ends.
func MakeRaw(fd int) (restore func() error, err error) {
	old, err := getTermios(fd)
	if err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ECHONL | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.INLCR | syscall.IGNCR | syscall.ISTRIP | syscall.BRKINT
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := setTermios(fd, raw); err != nil {
		return nil, err
	}
	return func() error { return setTermios(fd, old) }, nil
}
