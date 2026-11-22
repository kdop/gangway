package terminal

import "github.com/kdop/gangway/shared/rawterm"

// IsTerminal tells whether the file is a terminal.
func IsTerminal(fd int) bool { return rawterm.IsTerminal(fd) }

// MakeRaw sets the terminal so that every key comes at once, with no
// Enter, is not shown, and Ctrl+C comes as a key and not as a signal.
// restore puts the terminal back as it was found. It must be called before
// the program ends.
func MakeRaw(fd int) (restore func() error, err error) { return rawterm.MakeRaw(fd) }
