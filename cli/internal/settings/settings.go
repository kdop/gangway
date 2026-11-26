// Package settings gives every other piece its values: the starting
// values, and what the user's file changes. The design is in docs/design.md.
package settings

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kdop/gangway/shared/messages"
)

// Settings are the values a host can set.
type Settings struct {
	ControllerName     string        // CONTROLLER_TAILSCALE_NAME
	JoinTimeout        time.Duration // JOIN_TIMEOUT
	CommandShell       string        // COMMAND_SHELL
	CommandTimeLimit   time.Duration // COMMAND_TIME_LIMIT
	QuestionWait       time.Duration // QUESTION_WAIT
	KillWait           time.Duration // KILL_WAIT
	PromptMinShow      time.Duration // PROMPT_MIN_SHOW
	DoubleCtrlCWindow  time.Duration // DOUBLE_CTRL_C_WINDOW
	MaxWaitingRequests int           // MAX_WAITING_REQUESTS
	SessionLength      time.Duration // SESSION_LENGTH
	ExtendWait         time.Duration // EXTEND_WAIT
	LogFileSize        int64         // LOG_FILE_SIZE, in bytes
	LogFilesKept       int           // LOG_FILES_KEPT
}

// Default gives the starting values. They are the ones in the "Settings"
// table of docs/requirements.md.
func Default() Settings {
	return Settings{
		ControllerName:     messages.DefaultControllerName,
		JoinTimeout:        30 * time.Second,
		CommandShell:       "/bin/sh",
		CommandTimeLimit:   60 * time.Second,
		QuestionWait:       60 * time.Second,
		KillWait:           2 * time.Second,
		PromptMinShow:      500 * time.Millisecond,
		DoubleCtrlCWindow:  2 * time.Second,
		MaxWaitingRequests: 20,
		SessionLength:      30 * time.Minute,
		ExtendWait:         60 * time.Second,
		LogFileSize:        10 * 1000 * 1000,
		LogFilesKept:       5,
	}
}

// UserFile tells where the user's file is.
func UserFile() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gangway", "settings"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gangway", "settings"), nil
}

// Load gives the starting values, changed by the file at path. A file that
// is not there changes nothing. A file with a mistake gives an error and
// the starting values.
func Load(path string) (Settings, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	defer file.Close()

	s := Default()
	if err := parse(file, path, s.set); err != nil {
		return Default(), err
	}
	return s, nil
}

// parse reads the lines of a settings file and hands each setting to set.
// An error names the file and the line.
func parse(file io.Reader, path string, set func(name, value string) error) error {
	seen := map[string]bool{}
	lines := bufio.NewScanner(file)
	for number := 1; lines.Scan(); number++ {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fail := func(why string) error {
			return fmt.Errorf("%s, line %d: %s", path, number, why)
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			return fail("no \"=\" on the line")
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if seen[name] {
			return fail(name + " is set twice")
		}
		seen[name] = true
		if err := set(name, value); err != nil {
			return fail(err.Error())
		}
	}
	return lines.Err()
}

func (s *Settings) set(name, value string) error {
	switch name {
	case "CONTROLLER_TAILSCALE_NAME":
		if !deviceName(value) {
			return fmt.Errorf("%s must be a name of letters, digits and \"-\"", name)
		}
		s.ControllerName = value
	case "JOIN_TIMEOUT":
		return setTime(&s.JoinTimeout, name, value, false)
	case "COMMAND_SHELL":
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be a full path, from /", name)
		}
		s.CommandShell = value
	case "COMMAND_TIME_LIMIT":
		return setTime(&s.CommandTimeLimit, name, value, false)
	case "QUESTION_WAIT":
		return setTime(&s.QuestionWait, name, value, false)
	case "KILL_WAIT":
		return setTime(&s.KillWait, name, value, false)
	case "PROMPT_MIN_SHOW":
		return setTime(&s.PromptMinShow, name, value, true)
	case "DOUBLE_CTRL_C_WINDOW":
		return setTime(&s.DoubleCtrlCWindow, name, value, false)
	case "SESSION_LENGTH":
		return setTime(&s.SessionLength, name, value, false)
	case "EXTEND_WAIT":
		return setTime(&s.ExtendWait, name, value, false)
	case "MAX_WAITING_REQUESTS":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("%s must be a number, one or more", name)
		}
		s.MaxWaitingRequests = n
	case "LOG_FILE_SIZE":
		n, err := Size(value)
		if err != nil || n < smallestLogFile {
			return fmt.Errorf("%s must be a size with a unit, such as 500KB or 10MB, and 1KB or more", name)
		}
		s.LogFileSize = n
	case "LOG_FILES_KEPT":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > mostLogFiles {
			return fmt.Errorf("%s must be a number from 1 to %d", name, mostLogFiles)
		}
		s.LogFilesKept = n
	case "SESSION_MAX":
		folder, _ := adminPlace()
		return fmt.Errorf("%s can be set in %s only", name, filepath.Join(folder, "settings"))
	default:
		return fmt.Errorf("%q is not a setting", name)
	}
	return nil
}

func setTime(target *time.Duration, name, value string, zeroOK bool) error {
	d, err := Time(value)
	if err != nil {
		return fmt.Errorf("%s must be a time with a unit, such as 90s or 500ms", name)
	}
	if d < 0 || (d == 0 && !zeroOK) {
		return fmt.Errorf("%s must be more than zero", name)
	}
	*target = d
	return nil
}

// Time reads a time the way the settings file writes one: a number with a
// unit, such as 90s, 30m or 500ms.
func Time(value string) (time.Duration, error) {
	return time.ParseDuration(value)
}

// deviceName tells whether s can be a name on the Tailscale network.
func deviceName(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

const (
	// smallestLogFile is the least LOG_FILE_SIZE may be. An entry must
	// have room for its first line and the note that it was cut.
	smallestLogFile = 1000
	mostLogFiles    = 100
)

// Size reads a size the way the settings file writes one: a whole number
// with a unit, such as 500KB or 10MB. 1KB is 1000 bytes.
func Size(value string) (int64, error) {
	units := []struct {
		name  string
		bytes int64
	}{{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000}, {"B", 1}}
	for _, unit := range units {
		digits, found := strings.CutSuffix(value, unit.name)
		if !found {
			continue
		}
		digits = strings.TrimSpace(digits)
		n, err := strconv.ParseInt(digits, 10, 64)
		if err != nil || n < 0 || strings.HasPrefix(digits, "+") || n > (1<<62)/unit.bytes {
			break
		}
		return n * unit.bytes, nil
	}
	return 0, fmt.Errorf("%q is not a size", value)
}
