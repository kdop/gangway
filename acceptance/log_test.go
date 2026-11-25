package acceptance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// entryStart is how the first line of every entry of the log starts: the
// time, then a space.
const entryStart = `\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(Z|[+-]\d\d:\d\d) `

// logFolders gives the folders that the cli-tool made for logs.
func (w *world) logFolders() []string {
	w.t.Helper()
	entries, err := os.ReadDir(w.tmp)
	if err != nil {
		w.t.Fatal(err)
	}
	var folders []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gangway-") {
			folders = append(folders, filepath.Join(w.tmp, e.Name()))
		}
	}
	return folders
}

// logFile gives the log file of the session. There must be one log folder.
func (w *world) logFile() string {
	w.t.Helper()
	folders := w.logFolders()
	if len(folders) != 1 {
		w.t.Fatalf("want one log folder in the temp folder, got %v", folders)
	}
	return filepath.Join(folders[0], "session.txt")
}

// logFiles gives every file of the log, the oldest first.
func (w *world) logFiles() []string {
	w.t.Helper()
	file := w.logFile()
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		w.t.Fatal(err)
	}
	var old []int
	for _, e := range entries {
		var n int
		switch _, err := fmt.Sscanf(e.Name(), "session.%d.txt", &n); {
		case e.Name() == "session.txt":
		case err == nil && e.Name() == fmt.Sprintf("session.%d.txt", n):
			old = append(old, n)
		default:
			w.t.Fatalf("the log folder holds %q", e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(old)))
	var files []string
	for _, n := range old {
		files = append(files, filepath.Join(filepath.Dir(file), fmt.Sprintf("session.%d.txt", n)))
	}
	return append(files, file)
}

// log gives the whole log of the session, the oldest entry first.
func (w *world) log() string {
	w.t.Helper()
	var all strings.Builder
	for _, file := range w.logFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			w.t.Fatal(err)
		}
		all.Write(b)
	}
	return all.String()
}

// logHas fails the test unless the log holds a run of lines that fits
// this pattern. The pattern starts at the start of an entry.
func (w *world) logHas(pattern string) {
	w.t.Helper()
	w.until(fmt.Sprintf("the log holds %q", pattern), func() bool {
		return regexp.MustCompile(`(?m)^` + entryStart + pattern).MatchString(w.log())
	})
}

// end ends the session with Ctrl+C and waits for the cli-tool to end.
func (w *world) end() {
	w.t.Helper()
	w.press("\x03")
	w.ended()
}

// refusedNoPrompt asks for a command that is refused with no prompt.
func (w *world) refusedNoPrompt(mark string) {
	w.t.Helper()
	w.ask("true "+mark+" \x1b[2J").answer().refused(w.t, "control_character")
}

func TestLOG01_1_EveryRequestIsLoggedWithItsAnswerAndTheTime(t *testing.T) {
	w := newWorld(t)
	w.run("echo yes-ran").ran(t, 0)
	a := w.ask("echo not-wanted")
	w.prompt(2)
	w.press("n")
	a.answer().refused(t, "user")
	w.refusedNoPrompt("no-prompt")
	w.end()

	id := `(\S+)`
	w.logHas(id + ` request\n  > echo yes-ran\n` + entryStart + `\S+ approved\n` + entryStart + `\S+ ended: exit code 0\n  \| yes-ran\n`)
	w.logHas(id + ` request\n  > echo not-wanted\n` + entryStart + `\S+ refused: user\n`)
	w.logHas(id + ` request\n  > true no-prompt \x{fffd}\[2J\n` + entryStart + `\S+ refused: control_character\n`)
	// Each answer carries the ID of its request.
	text := w.log()
	for _, m := range regexp.MustCompile(`(?m)^`+entryStart+`(\S+) request$`).FindAllStringSubmatch(text, -1) {
		if got := regexp.MustCompile(`(?m)^`+entryStart+regexp.QuoteMeta(m[2])+` (refused|ended): `).FindAllString(text, -1); len(got) != 1 {
			t.Fatalf("the request %s has %d answers in the log:\n%s", m[2], len(got), text)
		}
	}
	if strings.Contains(text, w.key) {
		t.Fatal("the log holds the session key")
	}
}

func TestLOG01_2_ACommandThatWasKilledIsLoggedWithHowItEnded(t *testing.T) {
	w := limWorld(t)
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	a.answer().asksWaitOrStop(t)
	w.gone(pid)
	w.logHas(`\S+ ended: killed_no_answer\n`)

	b := w.start(long(w, "pid2"))
	pid = w.pid("pid2")
	w.press("\x03")
	b.answer().killed(t, "killed_by_user")
	w.gone(pid)
	w.logHas(`\S+ ended: killed_by_user\n`)
	if got := strings.Count(w.log(), " approved\n"); got != 2 {
		t.Fatalf("the log holds %d approved commands, want 2:\n%s", got, w.log())
	}
}

func TestLOG02_1_TheLogIsInANewFolderUnderTheTempFolder(t *testing.T) {
	w := newWorld(t)
	w.run("echo one").ran(t, 0)
	w.end()

	file := w.logFile()
	if !regexp.MustCompile(`^gangway-[0-9]+$`).MatchString(filepath.Base(filepath.Dir(file))) {
		t.Fatalf("the log folder is named %q", filepath.Base(filepath.Dir(file)))
	}
	if !strings.Contains(w.log(), "  > echo one\n") {
		t.Fatalf("the log does not hold the request:\n%s", w.log())
	}
	// Nothing of it is in the home folder.
	filepath.WalkDir(w.cliHome, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "> echo one") {
			t.Errorf("the home folder holds a log: %s", path)
		}
		return nil
	})
	for _, place := range []string{filepath.Join(w.cliHome, ".local", "state", "gangway")} {
		if _, err := os.Stat(place); err == nil {
			t.Errorf("%s was made", place)
		}
	}
}

func TestLOG02_2_EachSessionHasALogOfItsOwnUnderARandomName(t *testing.T) {
	w := newWorld(t)
	w.run("echo first-session").ran(t, 0)
	first := w.logFile()
	firstKey := w.key
	w.end()

	// A second session on the same host, with the same temp folder.
	tmp := w.tmp
	w.cliEnv = append(w.cliEnv, "TMPDIR="+tmp)
	w.startCli(nil)
	w.tmp = tmp
	w.run("echo second-session").ran(t, 0)
	w.end()

	folders := w.logFolders()
	if len(folders) != 2 {
		t.Fatalf("want two log folders, got %v", folders)
	}
	for _, folder := range folders {
		b, err := os.ReadFile(filepath.Join(folder, "session.txt"))
		if err != nil {
			t.Fatal(err)
		}
		want, other := "second-session", "first-session"
		if filepath.Join(folder, "session.txt") == first {
			want, other = other, want
		}
		if !strings.Contains(string(b), want) || strings.Contains(string(b), other) {
			t.Fatalf("%s holds the wrong session:\n%s", folder, b)
		}
		// The name tells nothing of a session key.
		name := strings.TrimPrefix(filepath.Base(folder), "gangway-")
		for _, key := range []string{firstKey, w.key} {
			for i := 0; i+6 <= len(name); i++ {
				if strings.Contains(key, name[i:i+6]) {
					t.Fatalf("the folder name %q holds a part of a session key", filepath.Base(folder))
				}
			}
		}
	}
}

func TestLOG02_3_TheScreenSaysWhereTheLogIsAndTheLogStays(t *testing.T) {
	w := newWorld(t)
	file := w.logFile()
	if !strings.HasSuffix(file, ".txt") {
		t.Fatalf("the log file is %s", file)
	}
	w.shows("\n  log       " + file + "\n")
	w.run("echo one").ran(t, 0)
	w.end()
	if !strings.HasSuffix(w.screen.text(), "\n  requests  1 run, 0 refused\n  log       "+file+"\n") {
		t.Fatalf("the screen does not end with the place of the log:\n%s", w.screen.text())
	}
	if !strings.Contains(w.log(), " session ended: user") {
		t.Fatalf("the log is not there after the end, or not whole:\n%s", w.log())
	}
}

func TestLOG03_1_TheLogHoldsTheOutput(t *testing.T) {
	w := newWorld(t)
	got := w.run("echo line-one; echo line-two")
	got.ran(t, 0)
	w.logHas(`\S+ ended: exit code 0\n  \| line-one\n  \| line-two\n`)
	if got.Output != "line-one\nline-two\n" {
		t.Fatalf("the AI got %q", got.Output)
	}
}

func TestLOG03_3_ARefusedRequestIsLoggedWithNoOutput(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo never-printed")
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	w.end()
	w.logHas(`\S+ request\n  > echo never-printed\n` + entryStart + `\S+ refused: user\n`)
	if strings.Contains(w.log(), "  | ") || strings.Contains(w.log(), " approved") {
		t.Fatalf("the log holds an output or an approval:\n%s", w.log())
	}
}

func TestLOG04_1_OnlyTheOwnerCanReadTheLog(t *testing.T) {
	w := newWorld(t)
	w.run("echo one").ran(t, 0)
	file := w.logFile()
	ownerOnly(t, filepath.Dir(file), 0o700)
	ownerOnly(t, file, 0o600)
	w.end()
	ownerOnly(t, filepath.Dir(file), 0o700)
	ownerOnly(t, file, 0o600)
}

// small is a log of small files, so that a test fills them fast.
const smallLogFile = 1000

func smallLog(t *testing.T, kept int) *world {
	return newWorld(t, "LOG_FILE_SIZE = 1000B", fmt.Sprintf("LOG_FILES_KEPT = %d", kept))
}

// fill sends requests that are refused with no prompt, each with a mark
// of its own, and gives the marks.
func (w *world) fill(from, to int) []string {
	w.t.Helper()
	var marks []string
	for i := from; i < to; i++ {
		mark := fmt.Sprintf("mark-%03d-%s", i, strings.Repeat("m", 150))
		w.refusedNoPrompt(mark)
		marks = append(marks, mark)
	}
	return marks
}

// fillUntil sends such requests until the log has this many files, and
// gives the marks.
func (w *world) fillUntil(files int) []string {
	w.t.Helper()
	var marks []string
	for i := 0; len(w.logFiles()) < files; i++ {
		if i == 40 {
			w.t.Fatalf("the log has %d files after %d requests", len(w.logFiles()), i)
		}
		marks = append(marks, w.fill(1000+i, 1001+i)...)
	}
	return marks
}

// wholeFiles fails the test if a log file is too large or does not start
// and end with a whole entry.
func (w *world) wholeFiles() {
	w.t.Helper()
	for _, file := range w.logFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			w.t.Fatal(err)
		}
		if len(b) > smallLogFile {
			w.t.Fatalf("%s has %d bytes", file, len(b))
		}
		if len(b) > 0 && (!regexp.MustCompile(`^`+entryStart).Match(b) || !strings.HasSuffix(string(b), "\n")) {
			w.t.Fatalf("%s holds half an entry:\n%s", file, b)
		}
	}
}

func TestLOG05_1_AFullLogFileIsKeptAndANewOneStarted(t *testing.T) {
	w := smallLog(t, 5)
	marks := w.fillUntil(2)
	files := w.logFiles()
	if len(files) != 2 || filepath.Base(files[0]) != "session.1.txt" {
		t.Fatalf("want the old file and the log file, got %v", files)
	}
	// The old file was full: one more request did not fit in it.
	if info, err := os.Stat(files[0]); err != nil || info.Size() < smallLogFile/2 || len(marks) < 2 {
		t.Fatalf("a new file was started too early, after %d requests", len(marks))
	}
	w.wholeFiles()
	text := w.log()
	for _, mark := range marks {
		if strings.Count(text, "  > true "+mark+" ") != 1 {
			t.Fatalf("the request %s is not in the log once:\n%s", mark[:8], text)
		}
	}
	if got := strings.Count(text, " refused: control_character\n"); got != len(marks) {
		t.Fatalf("the log holds %d answers, want %d", got, len(marks))
	}
}

func TestLOG05_2_TheOldestLogFileIsDeleted(t *testing.T) {
	w := smallLog(t, 2)
	marks := w.fillUntil(3)
	before := w.log()
	if !strings.Contains(before, marks[len(marks)-1]) {
		t.Fatal("the newest request is not in the log")
	}
	marks = append(marks, w.fill(9, 12)...)
	if got := w.logFiles(); len(got) != 3 {
		t.Fatalf("want two old files and the log file, got %v", got)
	}
	w.wholeFiles()
	text := w.log()
	if strings.Contains(text, marks[0]) {
		t.Fatal("the oldest request is still in the log")
	}
	// What is kept is the newest, with no gap.
	kept := false
	for _, mark := range marks {
		has := strings.Contains(text, "  > true "+mark+" ")
		if kept && !has {
			t.Fatalf("the request %s is gone, and an older one is kept", mark[:8])
		}
		kept = kept || has
	}
	if !strings.Contains(text, marks[len(marks)-1]) {
		t.Fatal("the newest request is not in the log")
	}
}

func TestLOG05_3_TheLogOfASessionNeverTakesMoreThanItsBound(t *testing.T) {
	const kept = 2
	w := smallLog(t, kept)
	size := func() int64 {
		total := int64(0)
		for _, file := range w.logFiles() {
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			total += info.Size()
		}
		return total
	}
	for i := 0; i < 30; i += 3 {
		w.fill(i, i+3)
		if got := size(); got > smallLogFile*(kept+1) {
			t.Fatalf("the log takes %d bytes", got)
		}
		if got := w.logFiles(); len(got) > kept+1 {
			t.Fatalf("the log has %d files", len(got))
		}
	}
	w.run("head -c 20000 /dev/zero | tr '\\0' x").ran(t, 0)
	w.end()
	if got := size(); got > smallLogFile*(kept+1) {
		t.Fatalf("the log takes %d bytes", got)
	}
	w.wholeFiles()
}

func TestLOG05_4_OnlyTheOwnerCanReadANewLogFile(t *testing.T) {
	w := smallLog(t, 2)
	w.fillUntil(3)
	w.fill(0, 6)
	files := w.logFiles()
	if len(files) != 3 {
		t.Fatalf("want two old files and the log file, got %v", files)
	}
	ownerOnly(t, filepath.Dir(files[0]), 0o700)
	for _, file := range files {
		ownerOnly(t, file, 0o600)
	}
}

func TestLOG05_5_AnEntryLargerThanOneFileIsCut(t *testing.T) {
	w := smallLog(t, 2)
	got := w.run("head -c 5000 /dev/zero | tr '\\0' x; echo; echo the-end")
	got.ran(t, 0)
	if !strings.HasSuffix(got.Output, "x\nthe-end\n") {
		t.Fatalf("the AI did not get the whole output: %d bytes", len(got.Output))
	}
	w.logHas(`\S+ ended: exit code 0\n  \| x+\n  \[cut: the log holds no more of this entry\]\n`)
	w.wholeFiles()
	if strings.Contains(w.log(), "| the-end") {
		t.Fatal("the log holds the end of an output that is larger than a file")
	}
}

func TestLOG06_1_NoSessionStartsWithoutALog(t *testing.T) {
	beside(t)
	ln := newWorld(t) // a controller
	missing := filepath.Join(t.TempDir(), "not-there")
	w := &world{t: t, home: t.TempDir(), listen: ln.listen, address: ln.address, ctl: ln.ctl, noPasteLine: true,
		cliEnv: []string{"TMPDIR=" + missing}}
	w.startCli(nil)
	w.ended()
	if code := w.cli.ProcessState.ExitCode(); code == 0 {
		t.Fatal("the cli-tool ended with exit code 0")
	}
	w.shows("cannot make the log of the session in " + missing)
	if strings.Contains(w.screen.text(), "/gangway ") {
		t.Fatal("a paste line was shown")
	}
}

func TestLOG06_2_ASessionEndsWhenItsLogCannotBeWritten(t *testing.T) {
	w := smallLog(t, 2)
	file := w.logFile()
	if err := os.RemoveAll(filepath.Dir(file)); err != nil {
		t.Fatal(err)
	}
	// The log file is full after a few requests. The new one cannot be
	// started, because the folder is gone.
	var last answer
	for i := 0; i < 6; i++ {
		last = w.ask(fmt.Sprintf("touch %s # %s \x1b", w.file("ran"), strings.Repeat("m", 150))).answer()
		if last.Reason != "control_character" {
			break
		}
	}
	// The request that could not be logged is refused. One that came after
	// the end finds no session.
	if last.Status != 401 {
		last.refused(t, "session_ended")
	}
	w.ended()
	w.shows("\n  ended     the log could not be written\n")
	w.shows("\n  log       " + file + "\n            The log is not whole: it could not be written.\n")
	if w.prompts() != 0 {
		t.Fatal("a prompt was shown")
	}
	w.notRun("ran")
}
