package terminal

import (
	"bytes"
	"fmt"
	"regexp"
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

// The prompt for reqA in the plain look, as the first request of a session.
var (
	headerA = "\n  -- request 1 " + strings.Repeat("-", 26) + "\n"
	promptA = headerA + "\n  > grep ERROR app.log | tail -n 20\n\n  run it?   y yes   n no "
)

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
	w.wantScreen(t, headerA+"\n  > cat > f <<'END'\n  > \tone\n  > END\n\n  run it?   y yes   n no ")
}

// UNIT-TERMINAL-13. Requirement: SHOW-01.
func TestUNITTERMINAL13_TheAnswer(t *testing.T) {
	for answer, want := range map[gate.Answer]string{
		gate.Yes:    "\n\n  -- request 1 -- running " + strings.Repeat("-", 15) + "  Ctrl+C stops it\n\n",
		gate.No:     "\n  x refused, nothing ran\n",
		gate.AIGone: "\n  x the AI hung up, nothing ran\n",
	} {
		w := newWorld()
		w.t.Prompt(reqA)
		w.t.Answer(reqA, answer)
		w.wantScreen(t, promptA+want)
	}
}

// UNIT-TERMINAL-14. Requirement: SHOW-02.
func TestUNITTERMINAL14_OutputAndTheLineAboutTheEnd(t *testing.T) {
	for _, code := range []int{0, 3} {
		w := newWorld()
		w.t.Output("one\ntwo\n")
		w.t.Ended(reqA, runner.Result{How: messages.HowExit, ExitCode: code})
		w.wantScreen(t, fmt.Sprintf("  | one\n  | two\n\n  + ended, exit %d, 2 lines sent to the AI\n", code))
	}
	w := newWorld()
	w.t.Output("one\n")
	w.t.Ended(reqA, runner.Result{How: messages.HowExit})
	w.t.Ended(reqA, runner.Result{How: messages.HowExit})
	w.wantScreen(t, "  | one\n\n  + ended, exit 0, 1 line sent to the AI\n\n  + ended, exit 0, no output\n")
}

// UNIT-TERMINAL-15. Requirement: SHOW-02.
func TestUNITTERMINAL15_EveryWayACommandEnds(t *testing.T) {
	seen := map[string]string{}
	for _, how := range []string{
		messages.HowExit, messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		w := newWorld()
		w.t.Ended(reqA, runner.Result{How: how})
		line := strings.TrimPrefix(w.screen.String(), "\n")
		sign := "  x "
		if how == messages.HowExit {
			sign = "  + "
		}
		if strings.Count(line, "\n") != 1 || len(line) < 10 || !strings.HasPrefix(line, sign) {
			t.Fatalf("%s shows %q, want one line of the cli-tool that starts with %q", how, line, sign)
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
	for _, look := range []Look{{}, {Unicode: true}, {Unicode: true, Color: true}} {
		w := newWorld()
		w.t.Dress(look)
		mark := look.outputMark()
		w.t.Output("one\ntwo\n\nfour\n")
		w.t.Output("half")
		w.t.Output(" a line\n")
		w.t.Output("x" + headerA + "\n  > ls\n\n  run it?   y yes   n no ")
		w.t.Output("\n  ✓ ended, exit 0\n  │ deeper\n/gangway http://x gw_1")
		want := mark + strings.Join([]string{
			"one", "two", "", "four", "half a line", "x", "  -- request 1 " + strings.Repeat("-", 26), "", "  > ls", "",
			"  run it?   y yes   n no ", "  ✓ ended, exit 0", "  │ deeper", "/gangway http://x gw_1",
		}, "\n"+mark)
		w.wantScreen(t, want)
		for _, l := range strings.Split(w.screen.String(), "\n") {
			if !strings.HasPrefix(l, mark) {
				t.Fatalf("the line %q has no mark", l)
			}
		}
	}
}

// UNIT-TERMINAL-19. Requirement: SHOW-04.
func TestUNITTERMINAL19_TheCliToolStartsOnANewLine(t *testing.T) {
	w := newWorld()
	w.t.Output("no line break")
	w.t.Ended(reqA, runner.Result{How: messages.HowExit})
	w.wantScreen(t, "  | no line break\n\n  + ended, exit 0, 1 line sent to the AI\n")

	w = newWorld()
	w.t.Output("  run it?   y yes   n no ")
	w.t.Prompt(reqA)
	w.wantScreen(t, "  |   run it?   y yes   n no \n"+promptA)
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
		if !strings.HasPrefix(l, "  x refused a request with no prompt: ") {
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
	if !strings.Contains(got, "\n  x refused a request") {
		t.Fatalf("the note does not start a line: %q", got)
	}
	w.t.Answer(reqA, gate.Yes)
	w.screen.Reset()
	w.t.Refused(messages.ReasonLineFull)
	if strings.Contains(w.screen.String(), "run it?") {
		t.Fatal("an answered prompt was shown again")
	}
}

// UNIT-TERMINAL-22. Requirement: KEY-04.
func TestUNITTERMINAL22_ThePasteLine(t *testing.T) {
	for _, look := range []Look{{}, {Unicode: true, Color: true}} {
		w := newWorld()
		w.t.Dress(look)
		w.t.Start(Start{Length: time.Minute, Address: "http://127.0.0.1:8642", Key: "gw_4f9c"})
		if !strings.Contains(w.screen.String(), "\n\n/gangway http://127.0.0.1:8642 gw_4f9c\n\n") {
			t.Fatalf("the screen shows %q, want the paste line on a line of its own, at the start of the line", w.screen.String())
		}
		if strings.Count(w.screen.String(), "gw_4f9c") != 1 {
			t.Fatalf("the key shows more than once: %q", w.screen.String())
		}
	}
}

// UNIT-TERMINAL-23. Requirement: SHOW-03.
func TestUNITTERMINAL23_EverythingIsCleaned(t *testing.T) {
	const bad = "\x1b[2J"
	w := newWorld()
	w.t.Start(Start{Host: "h" + bad, Network: "n" + bad, Length: time.Minute, Max: time.Minute, Asked: time.Hour, Admin: "/etc" + bad, Log: "/tmp" + bad, Address: "http://x" + bad, Key: "gw_" + bad})
	w.t.Note("note" + bad)
	w.t.Refused("why" + bad)
	w.t.Ended(reqA, runner.Result{How: "odd" + bad})
	w.t.Prompt(request.Request{ID: "a", Command: "ls" + bad})
	w.t.Over(Over{Log: "/tmp" + bad})
	if strings.ContainsRune(w.screen.String(), 0x1b) {
		t.Fatalf("an Escape reached the screen: %q", w.screen.String())
	}
	if strings.Count(w.screen.String(), "�[2J") != 11 {
		t.Fatalf("not every text from outside is on the screen with a mark in it: %q", w.screen.String())
	}
}

var extendQuestion = "\n  -- time is up " + strings.Repeat("-", 25) + "  answer in 60 s\n\n  extend by 30 min?   y yes   n no "

// UNIT-TERMINAL-28. Requirement: LEN-03.
func TestUNITTERMINAL28_TheQuestionToExtend(t *testing.T) {
	for answer, want := range map[session.Answer]string{
		session.Yes:      "\n  + extended by 30 min\n",
		session.No:       "\n  x not extended\n",
		session.NoAnswer: "\n  x no answer in time, not extended\n",
	} {
		w := newWorld()
		w.t.ExtendQuestion(30*time.Minute, false, time.Minute)
		w.wantScreen(t, extendQuestion)
		w.t.ExtendAnswer(answer, 30*time.Minute, false)
		w.wantScreen(t, extendQuestion+want)
	}
}

// UNIT-TERMINAL-29. Requirement: LEN-03, LEN-06.
func TestUNITTERMINAL29_TheQuestionToExtendStaysLast(t *testing.T) {
	w := newWorld()
	w.t.ExtendQuestion(30*time.Minute, false, time.Minute)
	w.t.Refused(messages.ReasonTimeUp)
	got := w.screen.String()
	if !strings.HasSuffix(got, extendQuestion) || strings.Count(got, extendQuestion) != 2 {
		t.Fatalf("the screen shows %q, want the question again at the end", got)
	}
	if !strings.Contains(got, "\n  x refused a request with no prompt: the session's time is up\n") {
		t.Fatalf("the note does not say why, on a line of its own: %q", got)
	}
	// The question shown again tells how long it still waits.
	w.pass(20*time.Second + 300*time.Millisecond)
	w.t.Note("a note")
	if !strings.HasSuffix(w.screen.String(), "  answer in 40 s\n\n  extend by 30 min?   y yes   n no ") {
		t.Fatalf("the question shown again does not tell how long it still waits: %q", w.screen.String())
	}
	w.t.Forget()
	w.screen.Reset()
	w.t.Note("The session ends.")
	if strings.Contains(w.screen.String(), "extend by") {
		t.Fatal("a forgotten question was shown again")
	}
	w.t.ExtendAnswer(session.Yes, 30*time.Minute, false)
	if strings.Contains(w.screen.String(), "extended") {
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
	header := "\n  -- time is up " + strings.Repeat("-", 25)
	question := header + "  answer in 60 s\n\n  extend by 5 min, up to the maximum?   y yes   n no "
	w := newWorld()
	w.t.ExtendQuestion(5*time.Minute, true, time.Minute)
	w.wantScreen(t, question)
	w.t.ExtendAnswer(session.Yes, 4*time.Minute+58*time.Second+200*time.Millisecond, true)
	w.wantScreen(t, question+"\n  + extended by 4 min 59 s, up to the maximum: then the session ends\n")

	w = newWorld()
	w.t.ExtendQuestion(1500*time.Millisecond, true, 1500*time.Millisecond)
	w.wantScreen(t, header+"  answer in 2 s\n\n  extend by 2 s, up to the maximum?   y yes   n no ")
	w.t.ExtendAnswer(session.AtMax, 0, false)
	if got := w.screen.String(); !strings.HasSuffix(got, "n no \n") {
		t.Fatalf("the question was not closed with a line break only: %q", got)
	}
	w.t.Note("The session ends.")
	if strings.Count(w.screen.String(), "extend by") != 1 {
		t.Fatal("a closed question was shown again")
	}
}

// UNIT-TERMINAL-31. Requirement: LEN-07, LEN-08.
func TestUNITTERMINAL31_TheTimeIsUpAtAPromptAndAtACommand(t *testing.T) {
	w := newWorld()
	w.t.Prompt(reqA)
	w.t.Answer(reqA, gate.TimeUp)
	w.wantScreen(t, promptA+"\n  x the session's time is up, nothing ran\n")
	w.screen.Reset()
	w.t.Note("a note")
	if strings.Contains(w.screen.String(), "run it?") {
		t.Fatal("the refused prompt was shown again")
	}

	w = newWorld()
	w.t.Ended(reqA, runner.Result{How: messages.HowKilledTimeUp})
	line := strings.TrimPrefix(w.screen.String(), "\n")
	if strings.Count(line, "\n") != 1 || !strings.HasPrefix(line, "  x killed: ") || !strings.Contains(line, "session's time was up") {
		t.Fatalf("the end shows %q, want one line that says it was killed and why", line)
	}
	for _, how := range []string{
		messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		other := newWorld()
		other.t.Ended(reqA, runner.Result{How: how})
		if strings.TrimPrefix(other.screen.String(), "\n") == line {
			t.Fatalf("%s shows the same line: %q", how, line)
		}
	}
}

// UNIT-TERMINAL-33. Requirement: KEY-04, LEN-01, SET-04, LOG-02.
func TestUNITTERMINAL33_TheStartOfASession(t *testing.T) {
	const paste = "\n  Paste this line to the AI:\n\n/gangway https://controller.example gw_4f9c\n\n  y run   n refuse   Ctrl+C end\n"
	start := Start{
		Host: "build-box", Network: "connected over Tailscale", Length: 30 * time.Minute,
		Log: "/tmp/gangway-1/session.txt", Address: "https://controller.example", Key: "gw_4f9c",
	}
	w := newWorld()
	w.t.Start(start)
	w.wantScreen(t, "\n"+
		"  host      build-box\n"+
		"  session   30 min, then you are asked to extend\n"+
		"  network   connected over Tailscale\n"+
		"  log       /tmp/gangway-1/session.txt\n"+paste)

	// A length below the maximum. No log, no host and no network: no such rows.
	w = newWorld()
	w.t.Start(Start{Length: 15 * time.Minute, Max: 20 * time.Minute, Address: start.Address, Key: start.Key})
	w.wantScreen(t, "\n  session   15 min, then you are asked to extend, up to the maximum of 20 min\n"+paste)

	// A length that the maximum cut.
	w = newWorld()
	start.Length, start.Max, start.Asked, start.Admin = 20*time.Minute, 20*time.Minute, time.Hour, "/etc/gangway/settings"
	w.t.Start(start)
	w.wantScreen(t, "\n"+
		"  host      build-box\n"+
		"  session   20 min, the maximum: then it ends\n"+
		"            1 h is above the maximum: it was cut to 20 min\n"+
		"            the maximum is set in /etc/gangway/settings\n"+
		"  network   connected over Tailscale\n"+
		"  log       /tmp/gangway-1/session.txt\n"+paste)
}

// UNIT-TERMINAL-34. Requirement: KEY-03, LOG-02.
func TestUNITTERMINAL34_TheEndOfASession(t *testing.T) {
	header := "\n  -- session over " + strings.Repeat("-", 23) + "\n\n"
	over := Over{Cause: CtrlC, Lasted: 42*time.Minute + 17*time.Second, Ran: 7, Refused: 1, Log: "/tmp/gangway-1/session.txt"}
	w := newWorld()
	w.t.Over(over)
	w.wantScreen(t, header+
		"  reason    Ctrl+C\n"+
		"  duration  42 min\n"+
		"  requests  7 run, 1 refused\n"+
		"  log       /tmp/gangway-1/session.txt\n")

	for name, c := range map[string]struct {
		change func(*Over)
		want   string
	}{
		"a log of more than one file": {func(o *Over) { o.LogOld = true },
			"  log       /tmp/gangway-1/session.txt\n            That file holds the last part. The parts before it are in the same folder.\n"},
		"a log that failed": {func(o *Over) { o.LogOld, o.LogBad = true, true },
			"  log       /tmp/gangway-1/session.txt\n            The log is not whole: it could not be written.\n"},
		"no request":  {func(o *Over) { o.Ran, o.Refused = 0, 0 }, "  requests  none\n"},
		"no log":      {func(o *Over) { o.Log = "" }, "  requests  7 run, 1 refused\n"},
		"a short one": {func(o *Over) { o.Lasted = 38*time.Second + 900*time.Millisecond }, "  duration  38 s\n"},
	} {
		o := over
		c.change(&o)
		w := newWorld()
		w.t.Over(o)
		if !strings.Contains(w.screen.String(), c.want) || strings.HasSuffix(w.screen.String(), "\n\n") {
			t.Fatalf("%s: the screen shows %q, want %q in it", name, w.screen.String(), c.want)
		}
	}

	for cause, want := range map[Cause]string{
		CtrlC: "Ctrl+C", Expired: "Expired", ControllerEnded: "Controller ended it", ConnectionLost: "Connection lost",
		LogFailed: "Log failed", StoppedFromOutside: "Stopped from outside", TerminalClosed: "Terminal closed",
		Unknown: "Unknown", Cause(99): "Unknown",
	} {
		w := newWorld()
		w.t.Over(Over{Cause: cause})
		if !strings.Contains(w.screen.String(), "\n  reason    "+want+"\n") {
			t.Fatalf("cause %d: the screen shows %q, want the reason %q", cause, w.screen.String(), want)
		}
	}

	// A prompt that waited is not shown again under the end.
	w = newWorld()
	w.t.Prompt(reqA)
	w.t.Forget()
	if !strings.HasSuffix(w.screen.String(), "n no \n  x the session ended, nothing ran\n") {
		t.Fatalf("the prompt that waited was not closed: %q", w.screen.String())
	}
	w.t.Over(over)
	if strings.Count(w.screen.String(), "run it?") != 1 {
		t.Fatalf("the prompt was shown again: %q", w.screen.String())
	}
}

// UNIT-TERMINAL-35. Requirement: SHOW-01, LEN-01.
func TestUNITTERMINAL35_TheHeaderOfAPrompt(t *testing.T) {
	w := newWorld()
	left, known := 27*time.Minute+59*time.Second, true
	w.t.TimeLeft(func() (time.Duration, bool) { return left, known })
	header := func() string {
		lines := strings.Split(w.screen.String(), "\n")
		w.screen.Reset()
		return lines[1]
	}
	w.t.Prompt(reqA)
	if got, want := header(), "  -- request 1 "+strings.Repeat("-", 26)+"  27 min left"; got != want {
		t.Fatalf("the header is %q, want %q", got, want)
	}
	// The same prompt shown again keeps its number, and reads the time again.
	left = 26 * time.Minute
	w.t.Note("a note")
	if got := strings.Split(w.screen.String(), "\n")[3]; !strings.HasPrefix(got, "  -- request 1 -") || !strings.HasSuffix(got, "  26 min left") {
		t.Fatalf("the header shown again is %q", got)
	}
	w.screen.Reset()
	w.t.Answer(reqA, gate.No)
	w.screen.Reset()

	for i, c := range []struct {
		left  time.Duration
		known bool
		want  string
	}{
		{time.Hour + 5*time.Minute + 10*time.Second, true, "  1 h 5 min left"},
		{42*time.Second + 500*time.Millisecond, true, "  42 s left"},
		{time.Minute, false, "--"},
		{0, true, "--"},
	} {
		left, known = c.left, c.known
		w.t.Prompt(reqA)
		got := header()
		if !strings.HasPrefix(got, fmt.Sprintf("  -- request %d -", i+2)) || !strings.HasSuffix(got, c.want) {
			t.Fatalf("with %v left, known %v, the header is %q, want request %d and %q at its end", c.left, c.known, got, i+2, c.want)
		}
		w.t.Answer(reqA, gate.No)
		w.screen.Reset()
	}
}

// session writes a whole session to the screen in this look: the start,
// requests whose commands hold cmd, output that holds out, every answer
// and every end.
func (w *world) session(look Look, cmd, out string) {
	w.t.Dress(look)
	Banner(&w.screen, look)
	Wait(&w.screen, "Joining Tailscale ...")
	w.t.TimeLeft(func() (time.Duration, bool) { return 27 * time.Minute, true })
	w.t.Start(Start{Host: "build-box", Network: "connected over Tailscale", Length: 20 * time.Minute, Max: 20 * time.Minute, Asked: time.Hour,
		Admin: "/etc/gangway/settings", Log: "/tmp/gangway-1/session.txt", Address: "https://controller.example", Key: "gw_4f9c"})
	req := request.Request{ID: "a", Command: cmd + " one\n" + cmd + " two"}
	w.t.Prompt(req)
	w.t.Refused(messages.ReasonLineFull)
	w.t.Note("a note")
	w.t.Answer(req, gate.Yes)
	w.t.Output(out + " one\n" + out)
	w.t.Output(" two\n\n" + out + " no end")
	w.t.Ended(req, runner.Result{How: messages.HowExit})
	for _, answer := range []gate.Answer{gate.No, gate.AIGone, gate.TimeUp} {
		w.t.Prompt(req)
		w.t.Answer(req, answer)
	}
	for _, how := range []string{
		messages.HowKilledByUser, messages.HowKilledByAI, messages.HowKilledNoAnswer, messages.HowKilledTimeUp,
		messages.HowKilledAIGone, messages.HowKilledSessionEnded, messages.HowStartFailed,
	} {
		w.t.Prompt(req)
		w.t.Answer(req, gate.Yes)
		w.t.Output(out + "\n")
		w.t.Ended(req, runner.Result{How: how})
	}
	for _, answer := range []session.Answer{session.Yes, session.No, session.NoAnswer, session.AtMax} {
		w.t.ExtendQuestion(5*time.Minute, answer == session.Yes, time.Minute)
		w.t.Refused(messages.ReasonTimeUp)
		w.t.ExtendAnswer(answer, 5*time.Minute, answer == session.Yes)
	}
	w.t.Prompt(req)
	w.t.Forget()
	w.t.Over(Over{Cause: CtrlC, Lasted: time.Hour, Ran: 8, Refused: 4, Log: "/tmp/gangway-1/session.txt", LogOld: true})
}

var colorCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// UNIT-TERMINAL-36. Requirement: SHOW-04, SHOW-06.
func TestUNITTERMINAL36_NoLineOfTheCliToolStartsWithAMark(t *testing.T) {
	for _, look := range []Look{{}, {Unicode: true}, {Color: true}, {Unicode: true, Color: true}} {
		w := newWorld()
		w.session(look, "CMD", "OUT")
		outputs, commands := 0, 0
		for _, l := range strings.Split(w.screen.String(), "\n") {
			switch {
			case strings.Contains(l, "OUT"):
				outputs++
				if !strings.HasPrefix(l, look.outputMark()+"OUT") {
					t.Fatalf("%+v: the line of output %q does not start with the mark", look, l)
				}
			case strings.Contains(l, "CMD"):
				commands++
				if !strings.HasPrefix(l, commandMark+"CMD") {
					t.Fatalf("%+v: the line of a command %q does not start with the mark", look, l)
				}
			default:
				// A line of the cli-tool itself, with its colors taken out.
				plain := colorCode.ReplaceAllString(l, "")
				for _, mark := range []string{"  | ", "  │ ", "  > ", "  |", "  │", "  >"} {
					if strings.HasPrefix(plain, mark) && l != look.outputMark() {
						t.Fatalf("%+v: the line %q of the cli-tool starts with a mark", look, l)
					}
				}
			}
		}
		if outputs < 10 || commands < 20 {
			t.Fatalf("%+v: the test saw %d lines of output and %d lines of commands: too few to prove anything", look, outputs, commands)
		}
	}
}

// UNIT-TERMINAL-37. Requirement: SHOW-06.
func TestUNITTERMINAL37_TheLookChangesSignsAndColorsOnly(t *testing.T) {
	screen := func(look Look) string {
		w := newWorld()
		w.session(look, "CMD", "OUT")
		return w.screen.String()
	}
	plain, signs := screen(Look{}), screen(Look{Unicode: true})

	// The plain look: ASCII only, and no code at all.
	for _, r := range plain {
		if r > '~' || (r < ' ' && r != '\n') {
			t.Fatalf("the plain look holds the character %q", r)
		}
	}
	if strings.ContainsRune(signs, 0x1b) {
		t.Fatal("a look with no color holds a code")
	}

	// The signs stand in for each other, one for one, and nothing else differs.
	back := strings.NewReplacer("─", "-", "│", "|", "✓", "+", "✗", "x").Replace(signs)
	if back != plain {
		t.Fatalf("the two looks differ in more than their signs:\n%q\n%q", back, plain)
	}
	for _, sign := range []string{"─", "│", "✓", "✗"} {
		if !strings.Contains(signs, sign) {
			t.Fatalf("the look with signs does not show %q", sign)
		}
	}

	// Colors: only codes that set a color or a weight, and the same text
	// with them taken out.
	for _, look := range []Look{{Color: true}, {Unicode: true, Color: true}} {
		colored := screen(look)
		if !colorCode.MatchString(colored) {
			t.Fatalf("%+v: no color was written", look)
		}
		without := colorCode.ReplaceAllString(colored, "")
		if strings.ContainsRune(without, 0x1b) {
			t.Fatalf("%+v: a code that is not a color was written: %q", look, without)
		}
		if want := screen(Look{Unicode: look.Unicode}); without != want {
			t.Fatalf("%+v: with the colors taken out the screen is not the one with no color:\n%q\n%q", look, without, want)
		}
	}
}

// UNIT-TERMINAL-38. Requirement: SHOW-06.
func TestUNITTERMINAL38_TheLookIsPickedFromTheSurroundings(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		want Look
	}{
		"a usual terminal":           {map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, Look{Unicode: true, Color: true}},
		"NO_COLOR":                   {map[string]string{"TERM": "xterm", "LANG": "en_US.UTF-8", "NO_COLOR": "1"}, Look{Unicode: true}},
		"a dumb terminal":            {map[string]string{"TERM": "dumb", "LANG": "en_US.UTF-8"}, Look{}},
		"no TERM":                    {map[string]string{"LANG": "en_US.UTF-8"}, Look{}},
		"a locale that is not UTF-8": {map[string]string{"TERM": "xterm", "LANG": "C"}, Look{Color: true}},
		"no locale":                  {map[string]string{"TERM": "xterm"}, Look{Color: true}},
		"LC_ALL comes first":         {map[string]string{"TERM": "xterm", "LC_ALL": "C", "LC_CTYPE": "en_US.UTF-8", "LANG": "en_US.UTF-8"}, Look{Color: true}},
		"LC_CTYPE comes before LANG": {map[string]string{"TERM": "xterm", "LC_CTYPE": "el_GR.utf8", "LANG": "C"}, Look{Unicode: true, Color: true}},
	} {
		c.want.Width = 80
		if got := Detect(func(name string) string { return c.env[name] }, 80); got != c.want {
			t.Fatalf("%s: the look is %+v, want %+v", name, got, c.want)
		}
	}
}

// UNIT-TERMINAL-39. Requirement: SHOW-06.
func TestUNITTERMINAL39_TheBanner(t *testing.T) {
	show := func(look Look) string {
		var b bytes.Buffer
		Banner(&b, look)
		return b.String()
	}
	whole := show(Look{})
	lines := strings.Split(strings.TrimSuffix(whole, "\n"), "\n")
	widest := 0
	for _, l := range lines {
		if len(l) > widest {
			widest = len(l)
		}
		for _, r := range l {
			if r > '~' || r < ' ' {
				t.Fatalf("the banner holds the character %q", r)
			}
		}
	}
	if len(lines) != 6 || widest < 20 {
		t.Fatalf("the banner is %q, want six lines", whole)
	}
	// Shown when the terminal is wider, and when its width is not known.
	if show(Look{Width: widest + 1}) != whole || show(Look{Width: 200}) != whole {
		t.Fatal("the banner is not shown on a terminal that is wide enough")
	}
	// Left out when it does not fit.
	for _, width := range []int{1, 20, widest - 1, widest} {
		if got := show(Look{Width: width}); got != "" {
			t.Fatalf("on a terminal of %d columns the banner shows: %q", width, got)
		}
	}
	if got := colorCode.ReplaceAllString(show(Look{Color: true}), ""); got != whole {
		t.Fatalf("the banner in color is not the same text: %q", got)
	}
}

// UNIT-TERMINAL-40. Requirement: LEN-03.
func TestUNITTERMINAL40_ATimeInShortForm(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute:               "30 min",
		time.Minute:                    "1 min",
		2 * time.Hour:                  "2 h",
		90 * time.Minute:               "1 h 30 min",
		90 * time.Second:               "1 min 30 s",
		time.Second:                    "1 s",
		time.Hour + 2*time.Second:      "1 h 2 s",
		1500 * time.Millisecond:        "1.5s",
		4*time.Minute + 58*time.Second: "4 min 58 s",
	} {
		if got := Short(d); got != want {
			t.Fatalf("%v is written %q, want %q", d, got, want)
		}
	}
	// A time that runs is rounded down, so the screen never promises more.
	for d, want := range map[time.Duration]string{
		27*time.Minute + 59*time.Second:            "27 min",
		time.Hour + 5*time.Minute + 59*time.Second: "1 h 5 min",
		59*time.Second + 999*time.Millisecond:      "59 s",
		time.Minute:                                "1 min",
		300 * time.Millisecond:                     "0 s",
	} {
		if got := about(d); got != want {
			t.Fatalf("%v that runs is written %q, want %q", d, got, want)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-TERMINAL", "terminal_test.go", "../../../shared/rawterm/rawterm_linux_test.go")
}
