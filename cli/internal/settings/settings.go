// Package settings gives every other piece its values: the starting
// values, and what the user's file changes. The design is in design.md.
package settings

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kdop/gangway/shared/messages"
)

// Settings are the values of the MVP.
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
}

// Default gives the starting values. They are the ones in the "Settings"
// table of requirements.md.
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
	seen := map[string]bool{}
	lines := bufio.NewScanner(file)
	for number := 1; lines.Scan(); number++ {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fail := func(why string) (Settings, error) {
			return Default(), fmt.Errorf("%s, line %d: %s", path, number, why)
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
		if err := s.set(name, value); err != nil {
			return fail(err.Error())
		}
	}
	if err := lines.Err(); err != nil {
		return Default(), err
	}
	return s, nil
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
