package settings

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/shared/designcheck"
)

// write makes a settings file in a scratch folder and gives its path.
func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantError checks that the file gives an error that holds each of these
// words, and that no value from the file is used.
func wantError(t *testing.T, text string, words ...string) {
	t.Helper()
	got, err := Load(write(t, text))
	if err == nil {
		t.Fatalf("no error for the file:\n%s", text)
	}
	for _, w := range words {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("the error %q does not hold %q", err, w)
		}
	}
	if got != Default() {
		t.Fatalf("a value from a file with a mistake was used: %+v", got)
	}
}

// UNIT-SETTINGS-01. Requirement: LIM-06.
func TestUNITSETTINGS01_TheStartingValues(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "not-there"))
	if err != nil {
		t.Fatal(err)
	}
	if got != Default() {
		t.Fatalf("with no file the values are %+v, want the starting values", got)
	}

	// The starting values must be the ones in requirements.md.
	doc, err := os.ReadFile("../../../requirements.md")
	if err != nil {
		t.Fatal(err)
	}
	table := map[string]string{}
	for _, row := range regexp.MustCompile("(?m)^\\| `([A-Z_]+)` \\| ([^|]+) \\|").FindAllStringSubmatch(string(doc), -1) {
		table[row[1]] = strings.Trim(strings.TrimSpace(row[2]), "`")
	}
	for name, have := range map[string]string{
		"COMMAND_SHELL":             got.CommandShell,
		"COMMAND_TIME_LIMIT":        words(got.CommandTimeLimit),
		"QUESTION_WAIT":             words(got.QuestionWait),
		"KILL_WAIT":                 words(got.KillWait),
		"PROMPT_MIN_SHOW":           words(got.PromptMinShow),
		"DOUBLE_CTRL_C_WINDOW":      words(got.DoubleCtrlCWindow),
		"MAX_WAITING_REQUESTS":      "20",
		"CONTROLLER_TAILSCALE_NAME": got.ControllerName,
		"JOIN_TIMEOUT":              words(got.JoinTimeout),
	} {
		if table[name] != have {
			t.Errorf("%s starts at %q in the code and %q in requirements.md", name, have, table[name])
		}
	}
	if got.MaxWaitingRequests != 20 {
		t.Errorf("MAX_WAITING_REQUESTS starts at %d", got.MaxWaitingRequests)
	}
}

// words writes a time the way the table of requirements.md does.
func words(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + " seconds"
}

// UNIT-SETTINGS-02. Requirement: LIM-06.
func TestUNITSETTINGS02_TheFileChangesEveryValue(t *testing.T) {
	got, err := Load(write(t, `CONTROLLER_TAILSCALE_NAME=my-controller
JOIN_TIMEOUT=10s
COMMAND_SHELL=/bin/bash
COMMAND_TIME_LIMIT=90s
QUESTION_WAIT=45s
KILL_WAIT=5s
PROMPT_MIN_SHOW=250ms
DOUBLE_CTRL_C_WINDOW=3s
MAX_WAITING_REQUESTS=7
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{
		ControllerName:     "my-controller",
		JoinTimeout:        10 * time.Second,
		CommandShell:       "/bin/bash",
		CommandTimeLimit:   90 * time.Second,
		QuestionWait:       45 * time.Second,
		KillWait:           5 * time.Second,
		PromptMinShow:      250 * time.Millisecond,
		DoubleCtrlCWindow:  3 * time.Second,
		MaxWaitingRequests: 7,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// UNIT-SETTINGS-03. Requirement: LIM-06.
func TestUNITSETTINGS03_NotesAndSpaces(t *testing.T) {
	got, err := Load(write(t, "# a note\n\n   COMMAND_TIME_LIMIT   =   2m  \n\t# another\nMAX_WAITING_REQUESTS=3"))
	if err != nil {
		t.Fatal(err)
	}
	if got.CommandTimeLimit != 2*time.Minute || got.MaxWaitingRequests != 3 {
		t.Fatalf("got %+v", got)
	}
}

// UNIT-SETTINGS-04. Requirement: LIM-06.
func TestUNITSETTINGS04_AnUnknownName(t *testing.T) {
	wantError(t, "COMMAND_TIME_LIMIT=90s\nCOMAND_SHELL=/bin/bash\n", "line 2", "COMAND_SHELL")
}

// UNIT-SETTINGS-05. Requirement: LIM-06.
func TestUNITSETTINGS05_AValueThatDoesNotFit(t *testing.T) {
	for _, line := range []string{
		"COMMAND_TIME_LIMIT=90",
		"QUESTION_WAIT=0s",
		"KILL_WAIT=-2s",
		"DOUBLE_CTRL_C_WINDOW=soon",
		"MAX_WAITING_REQUESTS=many",
		"MAX_WAITING_REQUESTS=-1",
		"PROMPT_MIN_SHOW=-1s",
		"COMMAND_TIME_LIMIT=",
	} {
		wantError(t, "# first line\n"+line+"\n", "line 2")
	}
}

// UNIT-SETTINGS-06. Requirement: GATE-11.
func TestUNITSETTINGS06_TheShellMustBeAFullPath(t *testing.T) {
	wantError(t, "COMMAND_SHELL=sh\n", "line 1", "COMMAND_SHELL")
	wantError(t, "COMMAND_SHELL=\n", "line 1")
}

// UNIT-SETTINGS-07. Requirement: LIM-06.
func TestUNITSETTINGS07_ANameSetTwice(t *testing.T) {
	wantError(t, "KILL_WAIT=2s\nQUESTION_WAIT=30s\nKILL_WAIT=9s\n", "line 3", "twice")
}

// UNIT-SETTINGS-08. Requirement: LIM-06.
func TestUNITSETTINGS08_ALineWithNoEqualsSign(t *testing.T) {
	wantError(t, "KILL_WAIT 2s\n", "line 1")
}

// UNIT-SETTINGS-09. Requirement: LIM-06.
func TestUNITSETTINGS09_WhereTheFileIs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/config")
	if got, _ := UserFile(); got != "/somewhere/config/gangway/settings" {
		t.Fatalf("the file is %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if got, _ := UserFile(); got != "/home/someone/.config/gangway/settings" {
		t.Fatalf("the file is %q", got)
	}
}

// UNIT-SETTINGS-10. Requirement: GATE-13.
func TestUNITSETTINGS10_TheLineNeedsRoomForOne(t *testing.T) {
	wantError(t, "MAX_WAITING_REQUESTS=0\n", "line 1")
}

// UNIT-SETTINGS-11. Requirement: GATE-14.
func TestUNITSETTINGS11_PromptMinShowMayBeZero(t *testing.T) {
	got, err := Load(write(t, "PROMPT_MIN_SHOW=0s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.PromptMinShow != 0 {
		t.Fatalf("PROMPT_MIN_SHOW is %v, want 0", got.PromptMinShow)
	}
}

// UNIT-SETTINGS-12. Requirement: HOST-06.
func TestUNITSETTINGS12_TheControllersNameMustBeAName(t *testing.T) {
	for _, value := range []string{"", "My-Controller", "gangway controller", "http://gangway-controller", "gangway.example.com", "-gangway", "gangway-", "a\x1b[2J"} {
		wantError(t, "CONTROLLER_TAILSCALE_NAME="+value+"\n", "line 1")
	}
}

// UNIT-SETTINGS-13. Requirement: HOST-08.
func TestUNITSETTINGS13_TheControllersAddressIsNoSettingAnyMore(t *testing.T) {
	wantError(t, "CONTROLLER_ADDRESS=http://127.0.0.1:8642\n", "line 1", "CONTROLLER_ADDRESS")
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SETTINGS", "settings_test.go")
}
