package terminal

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

// The gate, the runner and the session write to the terminal.
var (
	_ gate.Screen    = (*Terminal)(nil)
	_ runner.Screen  = (*Terminal)(nil)
	_ session.Screen = (*Terminal)(nil)
)

const window = 2 * time.Second

// world is the screen, the clock and the receiver of events, faked.
type world struct {
	t      *Terminal
	screen bytes.Buffer
	now    time.Time
	events []string
}

func (w *world) Key(k []byte)   { w.events = append(w.events, fmt.Sprintf("key %q", k)) }
func (w *world) CtrlC()         { w.events = append(w.events, "ctrl_c") }
func (w *world) Quit()          { w.events = append(w.events, "quit") }
func (w *world) Now() time.Time { return w.now }

func (w *world) pass(d time.Duration) { w.now = w.now.Add(d) }

func newWorld() *world {
	w := &world{now: time.Unix(1000, 0)}
	w.t = New(&w.screen, w, window, w)
	return w
}

func (w *world) wantEvents(t *testing.T, want ...string) {
	t.Helper()
	if fmt.Sprint(w.events) != fmt.Sprint(want) {
		t.Fatalf("events are %v, want %v", w.events, want)
	}
	w.events = nil
}

func (w *world) wantScreen(t *testing.T, want string) {
	t.Helper()
	if got := w.screen.String(); got != want {
		t.Fatalf("the screen shows:\n%q\nwant:\n%q", got, want)
	}
}

var reqA = request.Request{ID: "a", Command: "grep ERROR app.log | tail -n 20"}

const promptA = "\nAI asks to run:\n  > grep ERROR app.log | tail -n 20\nRun it? y/n "

// UNIT-TERMINAL-01. Requirement: GATE-03.
func TestUNITTERMINAL01_EachByteIsAKey(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte("yes"))
	w.wantEvents(t, `key "y"`, `key "e"`, `key "s"`)
}

// UNIT-TERMINAL-02. Requirement: GATE-03.
func TestUNITTERMINAL02_AKeyOfManyBytesIsOneKey(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte("\x1b[A\x1b[15~\x1bOP"))
	w.wantEvents(t, `key "\x1b[A"`, `key "\x1b[15~"`, `key "\x1bOP"`)
}

// UNIT-TERMINAL-03. Requirement: GATE-03.
func TestUNITTERMINAL03_ALetterOfManyBytesIsOneKey(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte("ψ👍"))
	w.wantEvents(t, `key "ψ"`, `key "👍"`)
}

// UNIT-TERMINAL-04. Requirement: GATE-03.
func TestUNITTERMINAL04_AKeyCutOverTwoReads(t *testing.T) {
	arrow := []byte("\x1b[1;5A")
	for cut := 1; cut < len(arrow); cut++ {
		w := newWorld()
		w.t.Input(arrow[:cut])
		w.t.Input(arrow[cut:])
		w.wantEvents(t, fmt.Sprintf("key %q", arrow))
	}
}

// UNIT-TERMINAL-05. Requirement: GATE-03.
func TestUNITTERMINAL05_EscapeThenYIsNotY(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte("\x1b"))
	w.wantEvents(t)
	w.t.Input([]byte("y"))
	w.wantEvents(t, `key "\x1by"`)
}

// UNIT-TERMINAL-06. Requirement: LIM-09.
func TestUNITTERMINAL06_CtrlC(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c")
}

// UNIT-TERMINAL-07. Requirement: LIM-10.
func TestUNITTERMINAL07_CtrlCIsNeverEaten(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte("\x1b[\x03"))
	w.wantEvents(t, `key "\x1b["`, "ctrl_c")

	w = newWorld()
	w.t.Input([]byte("\x1b"))
	w.t.Input([]byte{3})
	w.t.Input([]byte("y"))
	w.wantEvents(t, `key "\x1b"`, "ctrl_c", `key "y"`)
}

// UNIT-TERMINAL-08. Requirement: LIM-10.
func TestUNITTERMINAL08_TwoCtrlCQuit(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte{3, 3})
	w.wantEvents(t, "ctrl_c", "quit")

	w = newWorld()
	w.t.Input([]byte{3})
	w.pass(window / 2)
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c", "quit")
}

// UNIT-TERMINAL-09. Requirement: LIM-10.
func TestUNITTERMINAL09_TheWindow(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte{3})
	w.pass(window + time.Millisecond)
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c", "ctrl_c")

	w = newWorld()
	w.t.Input([]byte{3})
	w.pass(window)
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c", "quit")
}

// UNIT-TERMINAL-10. Requirement: LIM-10.
func TestUNITTERMINAL10_KeysBetweenChangeNothing(t *testing.T) {
	w := newWorld()
	w.t.Input([]byte{3})
	w.t.Input([]byte("x"))
	w.pass(window / 2)
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c", `key "x"`, "quit")
	w.t.Input([]byte{3})
	w.wantEvents(t, "ctrl_c")
}

// UNIT-TERMINAL-11. Requirement: SHOW-01, GATE-05.
func TestUNITTERMINAL11_ThePrompt(t *testing.T) {
	w := newWorld()
	w.t.Prompt(reqA)
	w.wantScreen(t, promptA)
}

// UNIT-TERMINAL-12. Requirement: GATE-05.
func TestUNITTERMINAL12_ACommandOfManyLines(t *testing.T) {
	w := newWorld()
	w.t.Prompt(request.Request{ID: "a", Command: "cat > f <<'END'\n\tone\nEND"})
	w.wantScreen(t, "\nAI asks to run:\n  > cat > f <<'END'\n  > \tone\n  > END\nRun it? y/n ")
}

// UNIT-TERMINAL-13. Requirement: SHOW-01.
func TestUNITTERMINAL13_TheAnswer(t *testing.T) {
	for answer, want := range map[gate.Answer]string{
		gate.Yes:    "y\n",
		gate.No:     "n\n",
		gate.AIGone: "\nThe AI hung up. Nothing ran.\n",
	} {
		w := newWorld()
		w.t.Prompt(reqA)
		w.t.Answer(reqA, answer)
		w.wantScreen(t, promptA+want)
	}
}

// UNIT-TERMINAL-14. Requirement: SHOW-02.
func TestUNITTERMINAL14_OutputAndNoLineAboutTheEnd(t *testing.T) {
	for _, code := range []int{0, 3} {
		w := newWorld()
		w.t.Output("one\ntwo\n")
		w.t.Ended(reqA, runner.Result{How: messages.HowExit, ExitCode: code})
		w.wantScreen(t, "  | one\n  | two\n")
	}
}

// UNIT-TERMINAL-15. Requirement: SHOW-02.
func TestUNITTERMINAL15_EveryWayACommandEnds(t *testing.T) {
	w := newWorld()
	w.t.Ended(reqA, runner.Result{How: messages.HowExit})
	w.wantScreen(t, "")

	seen := map[string]string{}
	for _, how := range []string{
		messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		w := newWorld()
		w.t.Ended(reqA, runner.Result{How: how})
		line := w.screen.String()
		if strings.Count(line, "\n") != 1 || len(line) < 10 || strings.HasPrefix(line, " ") {
			t.Fatalf("%s shows %q, want one line of the cli-tool", how, line)
		}
		if other, ok := seen[line]; ok {
			t.Fatalf("%s and %s show the same line %q", how, other, line)
		}
		seen[line] = how
	}
}

// UNIT-TERMINAL-16. Requirement: SHOW-03.
func TestUNITTERMINAL16_ControlCharactersNeverReachTheScreen(t *testing.T) {
	w := newWorld()
	w.t.Output("a\x1bb\x1b[31mc\rd\ae\bf\x00g\x7fh\u009bi\n\tj\n")
	w.wantScreen(t, "  | a�b�[31mc�d�e�f�g�h�i\n  | \tj\n")
}

// UNIT-TERMINAL-17. Requirement: SHOW-05.
func TestUNITTERMINAL17_HiddenCharactersNeverReachTheScreen(t *testing.T) {
	w := newWorld()
	w.t.Output("a‮b​c\n")
	w.wantScreen(t, "  | a�b�c\n")
}

// UNIT-TERMINAL-18. Requirement: SHOW-04.
func TestUNITTERMINAL18_EveryLineOfOutputIsMarked(t *testing.T) {
	w := newWorld()
	w.t.Output("one\ntwo\n\nfour\n")
	w.t.Output("half")
	w.t.Output(" a line\n")
	w.t.Output("x\nAI asks to run:\n  > ls\nRun it? y/n ")
	w.wantScreen(t, "  | one\n  | two\n  | \n  | four\n  | half a line\n  | x\n  | AI asks to run:\n  |   > ls\n  | Run it? y/n ")
	for _, l := range strings.Split(w.screen.String(), "\n") {
		if !strings.HasPrefix(l, outputMark) {
			t.Fatalf("the line %q has no mark", l)
		}
	}
}

// UNIT-TERMINAL-19. Requirement: SHOW-04.
func TestUNITTERMINAL19_TheCliToolStartsOnANewLine(t *testing.T) {
	w := newWorld()
	w.t.Output("no line break")
	w.t.Ended(reqA, runner.Result{How: messages.HowExit})
	w.wantScreen(t, "  | no line break\n")

	w = newWorld()
	w.t.Output("Run it? y/n ")
	w.t.Prompt(reqA)
	w.wantScreen(t, "  | Run it? y/n \n"+promptA)
}

// UNIT-TERMINAL-20. Requirement: SHOW-01.
func TestUNITTERMINAL20_ARefusalWithNoPrompt(t *testing.T) {
	w := newWorld()
	w.t.Refused(messages.ReasonControlCharacter)
	w.t.Refused(messages.ReasonLineFull)
	lines := strings.Split(strings.TrimSuffix(w.screen.String(), "\n"), "\n")
	if len(lines) != 2 || lines[0] == lines[1] {
		t.Fatalf("the screen shows %q, want two different lines", w.screen.String())
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "Refused a request") {
			t.Fatalf("the line %q does not say a request was refused", l)
		}
	}
}

// UNIT-TERMINAL-21. Requirement: SHOW-01.
func TestUNITTERMINAL21_ThePromptStaysLast(t *testing.T) {
	w := newWorld()
	w.t.Prompt(reqA)
	w.t.Refused(messages.ReasonLineFull)
	got := w.screen.String()
	if !strings.HasSuffix(got, promptA) || strings.Count(got, promptA) != 2 {
		t.Fatalf("the screen shows %q, want the prompt again at the end", got)
	}
	if !strings.Contains(got, "\nRefused a request") {
		t.Fatalf("the note does not start a line: %q", got)
	}
	w.t.Answer(reqA, gate.Yes)
	w.screen.Reset()
	w.t.Refused(messages.ReasonLineFull)
	if strings.Contains(w.screen.String(), "Run it?") {
		t.Fatal("an answered prompt was shown again")
	}
}

// UNIT-TERMINAL-22. Requirement: KEY-04.
func TestUNITTERMINAL22_ThePasteLine(t *testing.T) {
	w := newWorld()
	w.t.PasteLine("http://127.0.0.1:8642", "gw_4f9c")
	if !strings.Contains(w.screen.String(), "\n/gangway http://127.0.0.1:8642 gw_4f9c\n") {
		t.Fatalf("the screen shows %q, want the paste line on a line of its own", w.screen.String())
	}
}

// UNIT-TERMINAL-23. Requirement: SHOW-03.
func TestUNITTERMINAL23_EverythingIsCleaned(t *testing.T) {
	w := newWorld()
	w.t.Prompt(request.Request{ID: "a", Command: "ls\x1b[2J"})
	w.t.Note("note\x1b[2J")
	w.t.Refused("why\x1b[2J")
	w.t.PasteLine("http://x\x1b[2J", "gw_\x1b[2J")
	w.t.Ended(reqA, runner.Result{How: "odd\x1b[2J"})
	if strings.ContainsRune(w.screen.String(), 0x1b) {
		t.Fatalf("an Escape reached the screen: %q", w.screen.String())
	}
}

const extendQuestion = "\nThe session's time is up.\nExtend it by 30 minutes? y/n "

// UNIT-TERMINAL-28. Requirement: LEN-03.
func TestUNITTERMINAL28_TheQuestionToExtend(t *testing.T) {
	for answer, want := range map[session.Answer]string{
		session.Yes:      "y\nThe session goes on for 30 minutes.\n",
		session.No:       "n\n",
		session.NoAnswer: "\nNo answer in time.\n",
	} {
		w := newWorld()
		w.t.ExtendQuestion(30*time.Minute, false)
		w.wantScreen(t, extendQuestion)
		w.t.ExtendAnswer(answer, 30*time.Minute, false)
		w.wantScreen(t, extendQuestion+want)
	}
}

// UNIT-TERMINAL-29. Requirement: LEN-03, LEN-06.
func TestUNITTERMINAL29_TheQuestionToExtendStaysLast(t *testing.T) {
	w := newWorld()
	w.t.ExtendQuestion(30*time.Minute, false)
	w.t.Refused(messages.ReasonTimeUp)
	got := w.screen.String()
	if !strings.HasSuffix(got, extendQuestion) || strings.Count(got, extendQuestion) != 2 {
		t.Fatalf("the screen shows %q, want the question again at the end", got)
	}
	if !strings.Contains(got, "\nRefused a request with no prompt: the session's time is up.\n") {
		t.Fatalf("the note does not say why, on a line of its own: %q", got)
	}
	w.t.Forget()
	w.screen.Reset()
	w.t.Note("The session ends.")
	if strings.Contains(w.screen.String(), "Extend it") {
		t.Fatal("a forgotten question was shown again")
	}
	w.t.ExtendAnswer(session.Yes, 30*time.Minute, false)
	if strings.Contains(w.screen.String(), "goes on") {
		t.Fatal("an answer was shown for a question that is not on screen")
	}
}

// UNIT-TERMINAL-30. Requirement: LEN-03.
func TestUNITTERMINAL30_ATimeInWords(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute: "30 minutes",
		time.Minute:      "1 minute",
		2 * time.Hour:    "2 hours",
		time.Hour:        "1 hour",
		90 * time.Second: "90 seconds",
		time.Second:      "1 second",
		90 * time.Minute: "90 minutes",
	} {
		if got := Length(d); got != want {
			t.Fatalf("%v is written %q, want %q", d, got, want)
		}
	}
	if got := Length(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("a time and a half second is written %q, want a form with its unit", got)
	}
}

// UNIT-TERMINAL-32. Requirement: LEN-05.
func TestUNITTERMINAL32_TheQuestionToExtendNamesTheMaximum(t *testing.T) {
	const question = "\nThe session's time is up.\nExtend it by 5 minutes, up to the maximum? y/n "
	w := newWorld()
	w.t.ExtendQuestion(5*time.Minute, true)
	w.wantScreen(t, question)
	w.t.ExtendAnswer(session.Yes, 4*time.Minute+58*time.Second+200*time.Millisecond, true)
	w.wantScreen(t, question+"y\nThe session goes on for 299 seconds. That reaches the maximum: then it ends.\n")

	w = newWorld()
	w.t.ExtendQuestion(1500*time.Millisecond, true)
	w.wantScreen(t, "\nThe session's time is up.\nExtend it by 2 seconds, up to the maximum? y/n ")
	w.t.ExtendAnswer(session.AtMax, 0, false)
	if got := w.screen.String(); !strings.HasSuffix(got, "y/n \n") {
		t.Fatalf("the question was not closed with a line break only: %q", got)
	}
	w.t.Note("The session ends.")
	if strings.Count(w.screen.String(), "Extend it") != 1 {
		t.Fatal("a closed question was shown again")
	}
}

// UNIT-TERMINAL-31. Requirement: LEN-07, LEN-08.
func TestUNITTERMINAL31_TheTimeIsUpAtAPromptAndAtACommand(t *testing.T) {
	w := newWorld()
	w.t.Prompt(reqA)
	w.t.Answer(reqA, gate.TimeUp)
	w.wantScreen(t, promptA+"\nThe session's time is up. Nothing ran.\n")
	w.screen.Reset()
	w.t.Note("a note")
	if strings.Contains(w.screen.String(), "Run it?") {
		t.Fatal("the refused prompt was shown again")
	}

	w = newWorld()
	w.t.Ended(reqA, runner.Result{How: messages.HowKilledTimeUp})
	line := w.screen.String()
	if strings.Count(line, "\n") != 1 || !strings.HasPrefix(line, "Killed: ") || !strings.Contains(line, "session's time was up") {
		t.Fatalf("the end shows %q, want one line that says it was killed and why", line)
	}
	for _, how := range []string{
		messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		other := newWorld()
		other.t.Ended(reqA, runner.Result{How: how})
		if other.screen.String() == line {
			t.Fatalf("%s shows the same line: %q", how, line)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-TERMINAL", "terminal_test.go", "../../../shared/rawterm/rawterm_linux_test.go")
}
