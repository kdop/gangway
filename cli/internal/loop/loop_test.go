package loop

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/connection"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/cli/internal/settings"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

// The real connection fits the loop.
var _ Conn = (*connection.Conn)(nil)

// world is every edge of the cli-tool, in the test's hands. The pieces
// between the edges are the real ones.
type world struct {
	l *Loop

	screen bytes.Buffer
	now    time.Time
	sent   []string // every message to the controller, in order
	starts []string // the commands that were started
	stops  int
	kills  int
	timers []int
	closed []string
}

func (w *world) Now() time.Time { return w.now }

func (w *world) Started(id string)         { w.sent = append(w.sent, "started "+id) }
func (w *world) Refused(id, reason string) { w.sent = append(w.sent, "refused "+id+" "+reason) }
func (w *world) Output(id, text string)    { w.sent = append(w.sent, "output "+id+" "+text) }
func (w *world) StillRunning(id string)    { w.sent = append(w.sent, "still_running "+id) }
func (w *world) Ended(id string, r runner.Result) {
	w.sent = append(w.sent, fmt.Sprintf("ended %s %s %d", id, r.How, r.ExitCode))
}
func (w *world) Close(reason string) { w.closed = append(w.closed, reason) }

func (w *world) Start(shell, command string) (runner.Process, error) {
	w.starts = append(w.starts, command)
	return w, nil
}
func (w *world) Stop() { w.stops++ }
func (w *world) Kill() { w.kills++ }

type timers struct{ w *world }

func (t timers) Start(d time.Duration, number int) { t.w.timers = append(t.w.timers, number) }

func newWorld(t *testing.T, change func(*settings.Settings)) *world {
	t.Helper()
	s := settings.Default()
	if change != nil {
		change(&s)
	}
	sess, err := session.New(256)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.SetAddress("http://127.0.0.1:8642"); err != nil {
		t.Fatal(err)
	}
	w := &world{now: time.Unix(1000, 0)}
	w.l = New(Parts{Settings: s, Session: sess, Conn: w, Screen: &w.screen, Clock: w, Starter: w, Timers: timers{w}})
	return w
}

func live(t *testing.T) *world { return newWorld(t, nil) }

func req(id string) request.Request {
	return request.Request{ID: id, Command: "echo " + id}
}

// ask sends a request and lets PROMPT_MIN_SHOW pass.
func (w *world) ask(id string) {
	w.l.Request(req(id))
	w.pass(time.Second)
}

func (w *world) pass(d time.Duration) { w.now = w.now.Add(d) }
func (w *world) press(keys string)    { w.l.Input([]byte(keys)) }

// prompts counts how often the prompt for this request was drawn.
func (w *world) prompts(id string) int {
	return strings.Count(w.screen.String(), "  > echo "+id+"\nRun it? y/n ")
}

func (w *world) wantSent(t *testing.T, want ...string) {
	t.Helper()
	if fmt.Sprint(w.sent) != fmt.Sprint(want) {
		t.Fatalf("sent %q, want %q", w.sent, want)
	}
}

func (w *world) wantStarts(t *testing.T, want ...string) {
	t.Helper()
	if fmt.Sprint(w.starts) != fmt.Sprint(want) {
		t.Fatalf("started %q, want %q", w.starts, want)
	}
}

// PIE-01: a request that checks refuses never reaches the line or the gate.
func TestPIE01_ARefusedRequestGoesNoFurther(t *testing.T) {
	w := live(t)
	w.l.Request(request.Request{ID: "bad", Command: "ls\x1b[2J"})
	w.wantSent(t, "refused bad control_character")
	if strings.Contains(w.screen.String(), "Run it?") {
		t.Fatal("a prompt was drawn")
	}
	if strings.ContainsRune(w.screen.String(), 0x1b) || strings.Contains(w.screen.String(), "[2J") {
		t.Fatal("the refused text reached the screen")
	}
	// The line is no longer for it: a full line of good requests still fits.
	for i := 0; i < 20; i++ {
		w.l.Request(req(fmt.Sprint("r", i)))
	}
	if len(w.sent) != 1 {
		t.Fatalf("a good request was refused: %q", w.sent)
	}
}

// PIE-02: the gate gets the next request only when the command is over.
func TestPIE02_TheNextPromptWaitsForTheCommand(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.ask("b")
	if w.prompts("b") != 0 {
		t.Fatal("the prompt for b was drawn while a ran")
	}
	w.l.Exited(0)
	if w.prompts("b") != 1 {
		t.Fatal("the prompt for b was not drawn after a ended")
	}
	w.wantStarts(t, "echo a")
}

// PIE-04: output reaches the screen cleaned, and the connection as it was.
func TestPIE04_OutputIsCleanedForTheScreenOnly(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.l.Output([]byte("red\x1b[31m\n"))
	if !strings.Contains(w.screen.String(), "  | red�[31m\n") {
		t.Fatalf("the screen shows %q", w.screen.String())
	}
	if strings.ContainsRune(w.screen.String(), 0x1b) {
		t.Fatal("an Escape reached the screen")
	}
	w.wantSent(t, "started a", "output a red\x1b[31m\n")
}

// PIE-06: a key before the request does not answer it.
func TestPIE06_TheOrderOfKeyAndRequest(t *testing.T) {
	w := live(t)
	w.press("y")
	w.ask("a")
	w.wantStarts(t)
	if w.prompts("a") != 1 {
		t.Fatal("the prompt does not wait")
	}

	w = live(t)
	w.ask("a")
	w.press("y")
	w.wantStarts(t, "echo a")
}

// PIE-12: a y in the first moment of a prompt is dropped.
func TestPIE12_AnEarlyYIsDropped(t *testing.T) {
	w := live(t)
	w.l.Request(req("a"))
	w.press("y")
	w.wantSent(t)
	w.wantStarts(t)
	w.pass(settings.Default().PromptMinShow)
	w.press("y")
	w.wantSent(t, "started a")
	w.wantStarts(t, "echo a")
}

// PIE-13: a y just after a cancel does not answer the next prompt.
func TestPIE13_AYAfterACancelDoesNotRunTheNext(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.ask("b")
	w.l.Cancel("a")
	w.press("y")
	if w.prompts("b") != 1 {
		t.Fatal("the prompt for b was not drawn")
	}
	w.wantStarts(t)
	w.wantSent(t, "refused a ai_gone")
}

// PIE-14: PROMPT_MIN_SHOW from the settings reaches the gate.
func TestPIE14_PromptMinShowComesFromTheSettings(t *testing.T) {
	w := newWorld(t, func(s *settings.Settings) { s.PromptMinShow = 3 * time.Second })
	w.l.Request(req("a"))
	w.pass(3*time.Second - time.Millisecond)
	w.press("y")
	w.wantStarts(t)
	w.pass(time.Millisecond)
	w.press("y")
	w.wantStarts(t, "echo a")
}

// UNIT-LOOP-01. Requirement: GATE-01, SHOW-01.
func TestUNITLOOP01_OneRequestFromStartToEnd(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.l.Output([]byte("a\n"))
	w.l.Exited(0)
	w.wantSent(t, "started a", "output a a\n", "ended a exit 0")
	w.wantStarts(t, "echo a")
	want := "\nAI asks to run:\n  > echo a\nRun it? y/n y\n  | a\n"
	if w.screen.String() != want {
		t.Fatalf("the screen shows %q, want %q", w.screen.String(), want)
	}
}

// UNIT-LOOP-02. Requirement: GATE-13, SHOW-01.
func TestUNITLOOP02_AFullLine(t *testing.T) {
	w := newWorld(t, func(s *settings.Settings) { s.MaxWaitingRequests = 2 })
	w.ask("a")
	w.ask("b")
	w.ask("c")
	w.wantSent(t, "refused c line_full")
	if !strings.Contains(w.screen.String(), "too many requests wait") {
		t.Fatal("the screen does not say why")
	}
	if !strings.HasSuffix(w.screen.String(), "  > echo a\nRun it? y/n ") {
		t.Fatalf("the prompt for a is not the last thing on screen: %q", w.screen.String())
	}
	w.press("n")
	w.ask("d")
	w.wantSent(t, "refused c line_full", "refused a user")
}

// UNIT-LOOP-03. Requirement: LIM-09.
func TestUNITLOOP03_CtrlCAtAPromptRefuses(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.ask("b")
	w.press("\x03")
	w.wantSent(t, "refused a user")
	if w.l.State() != Live || w.prompts("b") != 1 {
		t.Fatal("the session did not go on to b")
	}
}

// UNIT-LOOP-04. Requirement: LIM-08.
func TestUNITLOOP04_CtrlCAtACommandKillsIt(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.press("\x03")
	if w.stops != 1 {
		t.Fatalf("the command was asked to stop %d times, want 1", w.stops)
	}
	w.l.Exited(143)
	w.wantSent(t, "started a", "ended a killed_by_user 0")
	if w.l.State() != Live {
		t.Fatal("the session ended")
	}
	w.pass(time.Minute)
	w.ask("b")
	if w.prompts("b") != 1 {
		t.Fatal("no prompt for a new request")
	}
}

// UNIT-LOOP-05. Requirement: SES-02, KEY-03.
func TestUNITLOOP05_CtrlCWithNothingInFrontEndsTheSession(t *testing.T) {
	w := live(t)
	w.press("\x03")
	if w.l.State() != Over {
		t.Fatalf("state is %v, want Over", w.l.State())
	}
	if fmt.Sprint(w.closed) != "[user]" {
		t.Fatalf("closed %q, want [user]", w.closed)
	}
	if w.l.session.Live() {
		t.Fatal("the session is still live")
	}
}

// UNIT-LOOP-06. Requirement: LIM-10, GATE-01.
func TestUNITLOOP06_TwoCtrlCAtAPromptEndEverything(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.ask("b")
	w.press("\x03\x03")
	w.wantSent(t, "refused a user", "refused b session_ended")
	w.wantStarts(t)
	if w.l.State() != Over {
		t.Fatalf("state is %v, want Over", w.l.State())
	}
}

// UNIT-LOOP-07. Requirement: LIM-10, SES-05.
func TestUNITLOOP07_TwoCtrlCAtACommandStopItThenEnd(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.ask("b")
	w.press("\x03\x03")
	if w.stops != 1 {
		t.Fatalf("the command was asked to stop %d times, want 1", w.stops)
	}
	if w.l.State() != Ending || len(w.closed) != 0 {
		t.Fatal("the loop did not wait for the command to be gone")
	}
	w.l.Exited(143)
	if w.l.State() != Over || fmt.Sprint(w.closed) != "[user]" {
		t.Fatalf("state %v, closed %q", w.l.State(), w.closed)
	}
	w.wantSent(t, "started a", "refused b session_ended", "ended a killed_by_user 0")
	if w.prompts("b") != 0 {
		t.Fatal("a prompt was drawn after the end")
	}
}

// UNIT-LOOP-08. Requirement: SES-05, GATE-01.
func TestUNITLOOP08_ALostConnectionEndsTheSession(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.l.Lost()
	w.wantStarts(t)
	if w.l.State() != Over || fmt.Sprint(w.closed) != "[connection_lost]" {
		t.Fatalf("state %v, closed %q", w.l.State(), w.closed)
	}
	if !strings.Contains(w.screen.String(), "connection to the controller was lost") {
		t.Fatal("the screen does not say why")
	}

	w = live(t)
	w.ask("a")
	w.press("y")
	w.l.Lost()
	if w.stops != 1 || w.l.State() != Ending {
		t.Fatal("the running command was not stopped")
	}
	w.l.Exited(143)
	if w.l.State() != Over {
		t.Fatal("the loop is not over")
	}
}

// UNIT-LOOP-09. Requirement: SES-03.
func TestUNITLOOP09_NothingIsTakenAfterTheEnd(t *testing.T) {
	w := live(t)
	w.press("\x03")
	sent, screen := len(w.sent), w.screen.Len()
	w.ask("a")
	w.press("y")
	w.l.Cancel("a")
	w.l.Wait("a")
	w.l.Stop("a")
	w.l.SessionEnd("again")
	w.l.Lost()
	w.press("\x03\x03")
	if len(w.sent) != sent || w.screen.Len() != screen || len(w.starts) != 0 || len(w.closed) != 1 {
		t.Fatalf("something happened after the end: sent %q, closed %q", w.sent, w.closed)
	}
}

// UNIT-LOOP-10. Requirement: LIM-02.
func TestUNITLOOP10_WaitAndStopNeedTheRightID(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.l.Timer(w.timers[len(w.timers)-1])
	w.l.Stop("zz")
	w.l.Stop("")
	w.l.Wait("zz")
	if w.stops != 0 {
		t.Fatal("a stop for another request stopped the command")
	}
	timersBefore := len(w.timers)
	w.l.Wait("a")
	if len(w.timers) != timersBefore+1 {
		t.Fatal("wait for the right request did not start a new time limit")
	}
	w.l.Stop("a")
	if w.stops != 1 {
		t.Fatal("stop for the right request did not stop the command")
	}
}

// UNIT-LOOP-11. Requirement: GATE-12, LIM-07.
func TestUNITLOOP11_CancelFindsTheRequestWhereverItIs(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.ask("b")
	w.ask("c")
	w.l.Cancel("c") // waits in the line
	w.wantSent(t, "started a", "refused c ai_gone")
	w.l.Cancel("a") // runs
	if w.stops != 1 {
		t.Fatal("the running command was not stopped")
	}
	w.l.Exited(143)
	w.pass(time.Second)
	w.l.Cancel("b") // at the prompt
	w.wantSent(t, "started a", "refused c ai_gone", "ended a killed_ai_gone 0", "refused b ai_gone")
	w.wantStarts(t, "echo a")
	if w.l.State() != Live {
		t.Fatal("the session ended")
	}
}

// UNIT-LOOP-12. Requirement: KEY-04.
func TestUNITLOOP12_BeginShowsThePasteLine(t *testing.T) {
	w := live(t)
	w.l.Begin()
	line := "\n/gangway " + w.l.session.Address() + " " + w.l.session.Key() + "\n"
	if !strings.Contains(w.screen.String(), line) {
		t.Fatalf("the screen shows %q, want the paste line", w.screen.String())
	}
}

// UNIT-LOOP-13. Requirement: SES-05.
func TestUNITLOOP13_TheControllerEndsTheSession(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.l.SessionEnd("removed")
	w.wantSent(t, "refused a session_ended")
	if w.l.State() != Over || fmt.Sprint(w.closed) != "[removed]" {
		t.Fatalf("state %v, closed %q", w.l.State(), w.closed)
	}
	if strings.HasSuffix(w.screen.String(), "Run it? y/n ") || w.prompts("a") != 1 {
		t.Fatal("the prompt was shown again after the end")
	}
}

// UNIT-LOOP-14. Requirement: GATE-01, GATE-07.
func TestUNITLOOP14_NoMessageRunsACommand(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.l.Wait("a")
	w.l.Stop("a")
	w.l.Timer(0)
	w.l.Timer(1)
	w.l.Output([]byte("y"))
	w.l.Exited(0)
	w.l.Request(req("a"))
	w.wantStarts(t)
	if got := messages.TypeStarted; strings.Contains(strings.Join(w.sent, " "), got) {
		t.Fatalf("started was sent with no y: %q", w.sent)
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-LOOP", "loop_test.go")
}
