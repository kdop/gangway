package log

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

const stamp = "2026-01-02T03:04:05Z"

func at() *clock { return &clock{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)} }

// open gives a log in a scratch folder.
func open(t *testing.T, size int64, kept int) (*Log, string) {
	t.Helper()
	place := t.TempDir()
	l, err := Open(place, size, kept, at())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l, place
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantLog(t *testing.T, l *Log, want string) {
	t.Helper()
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, l.File()); got != want {
		t.Fatalf("the log holds:\n%s\nwant:\n%s", got, want)
	}
}

// files gives the names in the log's folder.
func files(t *testing.T, l *Log) []string {
	t.Helper()
	entries, err := os.ReadDir(l.folder)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func ask(id, command string) request.Request { return request.Request{ID: id, Command: command} }

// UNIT-LOG-01. Requirement: LOG-02, LOG-04.
func TestUNITLOG01_OpenMakesAFolderOfItsOwn(t *testing.T) {
	place := t.TempDir()
	// A loose mask must not loosen the rights.
	one, err := Open(place, 1000, 2, at())
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := Open(place, 1000, 2, at())
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()

	if one.File() == two.File() {
		t.Fatal("two logs share a file")
	}
	for _, l := range []*Log{one, two} {
		folder := filepath.Dir(l.File())
		if filepath.Dir(folder) != place {
			t.Fatalf("the folder %s is not in %s", folder, place)
		}
		if !regexp.MustCompile(`^gangway-[0-9]+$`).MatchString(filepath.Base(folder)) {
			t.Fatalf("the folder is named %q", filepath.Base(folder))
		}
		if filepath.Base(l.File()) != "session.txt" {
			t.Fatalf("the file is named %q", filepath.Base(l.File()))
		}
		info, err := os.Lstat(folder)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("the folder has the rights %v", info.Mode())
		}
		info, err = os.Lstat(l.File())
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("the file has the rights %v", info.Mode())
		}
		if got := files(t, l); len(got) != 1 {
			t.Fatalf("the folder holds %v", got)
		}
	}

	if _, err := Open(filepath.Join(place, "not-there"), 1000, 2, at()); err == nil {
		t.Fatal("a log was opened in a place that is not there")
	}
}

// UNIT-LOG-02. Requirement: LOG-01, LOG-03.
func TestUNITLOG02_ACommandThatRanIsWrittenStepByStep(t *testing.T) {
	l, _ := open(t, 100000, 2)
	l.Request(ask("r_1", "echo one\necho two"))
	l.Started("r_1")
	l.Output("r_1", "one\ntw")
	l.Output("r_1", "o\n")
	l.Ended("r_1", runner.Result{How: messages.HowExit, ExitCode: 3})
	wantLog(t, l, stamp+" r_1 request\n"+
		"  > echo one\n"+
		"  > echo two\n"+
		stamp+" r_1 approved\n"+
		stamp+" r_1 ended: exit code 3\n"+
		"  | one\n"+
		"  | two\n")
}

// UNIT-LOG-03. Requirement: LOG-01, LOG-03.
func TestUNITLOG03_ARefusedRequestHasNoOutput(t *testing.T) {
	l, _ := open(t, 100000, 2)
	l.Request(ask("r_1", "rm -rf x"))
	l.Refused("r_1", messages.ReasonUser)
	// Output for a request that was never approved is not held.
	l.Output("r_1", "never")
	l.Request(ask("r_2", "true"))
	l.Refused("r_2", messages.ReasonControlCharacter)
	wantLog(t, l, stamp+" r_1 request\n"+
		"  > rm -rf x\n"+
		stamp+" r_1 refused: user\n"+
		stamp+" r_2 request\n"+
		"  > true\n"+
		stamp+" r_2 refused: control_character\n")
}

// UNIT-LOG-04. Requirement: LOG-01.
func TestUNITLOG04_EveryWayToEndIsNamed(t *testing.T) {
	l, _ := open(t, 100000, 2)
	want := ""
	for _, how := range []string{
		messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer, messages.HowKilledTimeUp,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		l.Started("r_1")
		// The exit code counts for "exit" only.
		l.Ended("r_1", runner.Result{How: how, ExitCode: 7})
		want += stamp + " r_1 approved\n" + stamp + " r_1 ended: " + how + "\n"
	}
	l.Started("r_2")
	l.Output("r_2", "a�b")
	l.Ended("r_2", runner.Result{How: messages.HowExit, BytesReplaced: true})
	want += stamp + " r_2 approved\n" + stamp + " r_2 ended: exit code 0, bytes that are not text were replaced\n  | a�b\n"
	// The output of one command is not written with the next.
	l.Started("r_3")
	l.Ended("r_3", runner.Result{How: messages.HowExit})
	want += stamp + " r_3 approved\n" + stamp + " r_3 ended: exit code 0\n"
	wantLog(t, l, want)
}

// UNIT-LOG-05. Requirement: LOG-01.
func TestUNITLOG05_NoTextCanFakeAnEntry(t *testing.T) {
	l, _ := open(t, 100000, 2)
	fake := stamp + " r_9 refused: user"
	l.Request(ask("r 1\n"+fake, "true\n"+fake+"\x1b[2J\r‮"))
	l.Started("r_2")
	l.Output("r_2", fake+"\n\x1b[31mred\x07\n\n"+fake)
	l.Ended("r_2", runner.Result{How: "odd\nway\x1b"})
	l.Refused("", "why\n"+fake)
	l.Session("note\n" + fake)
	l.Request(ask(strings.Repeat("x", 500), "true"))

	text := read(t, l.File())
	first := regexp.MustCompile(`^` + regexp.QuoteMeta(stamp) + ` `)
	starts := 0
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		switch {
		case first.MatchString(line):
			starts++
		case strings.HasPrefix(line, "  > "), strings.HasPrefix(line, "  | "):
		default:
			t.Fatalf("the line %q starts like no line of the log", line)
		}
	}
	if starts != 6 {
		t.Fatalf("the log has %d entries, want 6:\n%s", starts, text)
	}
	for _, r := range text {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f || r == '‮') {
			t.Fatalf("the log holds the character %U", r)
		}
	}
	if !strings.HasPrefix(text, stamp+" r?1?"+fake[:len(stamp)]+"?r_9?refused:?user request\n") {
		t.Fatalf("the ID was not made plain:\n%s", text)
	}
	if !strings.Contains(text, "\n"+stamp+" ? refused: why "+fake+"\n") {
		t.Fatalf("an empty ID or a reason of two lines is wrong:\n%s", text)
	}
	if !strings.Contains(text, "\n"+stamp+" "+strings.Repeat("x", 64)+" request\n") {
		t.Fatalf("a long ID was not cut at 64:\n%s", text)
	}
}

// UNIT-LOG-06. Requirement: LOG-05.
func TestUNITLOG06_AFullFileIsKeptUnderAnotherName(t *testing.T) {
	l, _ := open(t, 1000, 2)
	line := stamp + " session " + strings.Repeat("a", 100) + "\n"
	// Seven entries fit under 1000 bytes, eight do not.
	for i := 0; i < 7; i++ {
		l.Session(strings.Repeat("a", 100))
	}
	if got := files(t, l); len(got) != 1 {
		t.Fatalf("a new file was started too early: %v", got)
	}
	l.Request(ask("r_1", strings.Repeat("b", 300)))
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(files(t, l), " "); got != "session.1.txt session.txt" {
		t.Fatalf("the folder holds %q", got)
	}
	if got := read(t, l.oldFile(1)); got != strings.Repeat(line, 7) {
		t.Fatalf("the old file holds:\n%s", got)
	}
	wantLog(t, l, stamp+" r_1 request\n  > "+strings.Repeat("b", 300)+"\n")
	if l.Old() != 1 {
		t.Fatalf("Old is %d", l.Old())
	}
}

// UNIT-LOG-07. Requirement: LOG-05, LOG-04.
func TestUNITLOG07_OnlyTheLastFilesAreKept(t *testing.T) {
	const size, kept = 1000, 3
	l, _ := open(t, size, kept)
	for i := 0; i < 400; i++ {
		l.Started("r_1")
		l.Output("r_1", strings.Repeat("o", i%700))
		l.Ended("r_1", runner.Result{How: messages.HowExit, ExitCode: i})
		if err := l.Err(); err != nil {
			t.Fatal(err)
		}
		names := files(t, l)
		if len(names) > kept+1 {
			t.Fatalf("after %d commands the folder holds %v", i, names)
		}
		total := int64(0)
		for _, name := range names {
			info, err := os.Lstat(filepath.Join(l.folder, name))
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() > size {
				t.Fatalf("%s has %d bytes", name, info.Size())
			}
			if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
				t.Fatalf("%s has the rights %v", name, info.Mode())
			}
			total += info.Size()
		}
		if total > size*(kept+1) {
			t.Fatalf("the files hold %d bytes", total)
		}
	}
	if got := strings.Join(files(t, l), " "); got != "session.1.txt session.2.txt session.3.txt session.txt" {
		t.Fatalf("the folder holds %q", got)
	}
	// The newest entry is in the log file, and the files go back in order.
	var codes []string
	for _, path := range []string{l.oldFile(3), l.oldFile(2), l.oldFile(1), l.File()} {
		text := read(t, path)
		if !strings.HasPrefix(text, stamp+" ") {
			t.Fatalf("%s starts in the middle of an entry", path)
		}
		for _, m := range regexp.MustCompile(`exit code (\d+)`).FindAllStringSubmatch(text, -1) {
			codes = append(codes, m[1])
		}
	}
	if codes[len(codes)-1] != "399" {
		t.Fatalf("the last entry is of command %s", codes[len(codes)-1])
	}
	for i := 1; i < len(codes); i++ {
		if len(codes[i]) < len(codes[i-1]) || (len(codes[i]) == len(codes[i-1]) && codes[i] <= codes[i-1]) {
			t.Fatalf("the entries are not in order: %v", codes)
		}
	}
}

// UNIT-LOG-08. Requirement: LOG-05.
func TestUNITLOG08_AnEntryIsNeverLongerThanOneFile(t *testing.T) {
	const size = 1000
	l, _ := open(t, size, 1)
	l.Session("first")
	l.Started("r_1")
	for i := 0; i < 5000; i++ {
		l.Output("r_1", "0123456789 é\n")
	}
	if held := l.output["r_1"].Len(); held > size+100 {
		t.Fatalf("%d bytes of output are held", held)
	}
	l.Ended("r_1", runner.Result{How: messages.HowExit})
	text := read(t, l.File())
	if len(text) > size {
		t.Fatalf("the entry has %d bytes", len(text))
	}
	if len(text) < size-50 {
		t.Fatalf("the entry has only %d bytes", len(text))
	}
	if !strings.HasPrefix(text, stamp+" r_1 ended: exit code 0\n  | 0123456789 é\n") {
		t.Fatalf("the entry starts wrong:\n%s", text)
	}
	if !strings.HasSuffix(text, "\n  [cut: the log holds no more of this entry]\n") {
		t.Fatalf("the entry does not end with the note:\n%s", text)
	}
	if strings.Contains(text, "�") {
		t.Fatal("a character was cut in half")
	}

	// A request can be longer than a file too.
	l.Request(ask("r_2", strings.Repeat("c", 3*size)))
	text = read(t, l.File())
	if len(text) > size || !strings.HasSuffix(text, "c\n  [cut: the log holds no more of this entry]\n") {
		t.Fatalf("a long request is written as %d bytes", len(text))
	}
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
}

// UNIT-LOG-09. Requirement: LOG-06.
func TestUNITLOG09_AFailureIsKeptAndNothingMoreIsWritten(t *testing.T) {
	l, _ := open(t, 1000, 2)
	l.Session("one")
	if l.Err() != nil {
		t.Fatal(l.Err())
	}
	// The file is closed behind the log's back: the next write fails.
	l.file.Close()
	l.Session("two")
	first := l.Err()
	if first == nil {
		t.Fatal("no failure for a write that failed")
	}
	l.Request(ask("r_1", "true"))
	l.Started("r_1")
	l.Ended("r_1", runner.Result{How: messages.HowExit})
	if l.Err() != first {
		t.Fatalf("the failure changed to %v", l.Err())
	}
	if got := read(t, l.File()); got != stamp+" session one\n" {
		t.Fatalf("the log holds:\n%s", got)
	}

	// A new file that cannot be started is a failure too.
	l, _ = open(t, 1000, 2)
	l.Session(strings.Repeat("a", 800))
	if err := os.Mkdir(l.oldFile(1), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.oldFile(1), "in-the-way"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l.Session(strings.Repeat("b", 800))
	if l.Err() == nil {
		t.Fatal("no failure when the full file could not be put aside")
	}
}

// UNIT-LOG-10. Requirement: LOG-02.
func TestUNITLOG10_CloseDeletesNothing(t *testing.T) {
	l, _ := open(t, 1000, 2)
	l.Session(strings.Repeat("a", 800))
	l.Session(strings.Repeat("b", 800))
	l.Close()
	l.Session("late")
	l.Close()
	if l.Err() != nil {
		t.Fatal(l.Err())
	}
	if got := strings.Join(files(t, l), " "); got != "session.1.txt session.txt" {
		t.Fatalf("the folder holds %q", got)
	}
	if got := read(t, l.File()); got != stamp+" session "+strings.Repeat("b", 800)+"\n" {
		t.Fatalf("the log holds:\n%s", got)
	}
}

// UNIT-LOG-11. Requirement: LOG-02.
func TestUNITLOG11_ThePlaceIsTheTempFolder(t *testing.T) {
	t.Setenv("TMPDIR", "/somewhere/else")
	if got := Place(); got != "/somewhere/else" {
		t.Fatalf("with TMPDIR set the place is %q", got)
	}
	t.Setenv("TMPDIR", "")
	if got := Place(); got != "/tmp" {
		t.Fatalf("with no TMPDIR the place is %q", got)
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-LOG", "log_test.go")
}
