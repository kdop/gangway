package loop

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/connection"
	"github.com/kdop/gangway/cli/internal/log"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/cli/internal/settings"
	"github.com/kdop/gangway/cli/internal/terminal"
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

	stimers []sessionTimer // the timers of the session's clock
}

type sessionTimer struct {
	d      time.Duration
	number int
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
func (w *world) TimeUp()             { w.sent = append(w.sent, "time_up") }
func (w *world) Extended(seconds int) {
	w.sent = append(w.sent, fmt.Sprint("extended ", seconds))
}

func (w *world) Start(shell, command string) (runner.Process, error) {
	w.starts = append(w.starts, command)
	return w, nil
}
func (w *world) Stop() { w.stops++ }
func (w *world) Kill() { w.kills++ }

type timers struct{ w *world }

func (t timers) Start(d time.Duration, number int) { t.w.timers = append(t.w.timers, number) }

type sessionTimers struct{ w *world }

func (t sessionTimers) Start(d time.Duration, number int) {
	t.w.stimers = append(t.w.stimers, sessionTimer{d, number})
}

func newWorld(t *testing.T, change func(*settings.Settings)) *world {
	t.Helper()
	return newWorldOf(t, change, 0)
}

// newWorldOf gives a world whose session was started with this length.
// Zero: no length was asked for.
func newWorldOf(t *testing.T, change func(*settings.Settings), length time.Duration) *world {
	t.Helper()
	return newWorldMax(t, change, length, 0)
}

// newWorldMax is newWorldOf on a host whose admin file sets this
// SESSION_MAX. Zero: none.
func newWorldMax(t *testing.T, change func(*settings.Settings), length, max time.Duration) *world {
	t.Helper()
	return newWorldLogged(t, change, length, max, nil)
}

// newWorldLogged is newWorldMax with this log. Nil: nothing is logged.
func newWorldLogged(t *testing.T, change func(*settings.Settings), length, max time.Duration, record Log) *world {
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
	w.l = New(Parts{Settings: s, Session: sess, Conn: w, Screen: &w.screen, Clock: w, Starter: w, Timers: timers{w},
		SessionTimers: sessionTimers{w}, Length: length,
		Admin: settings.Admin{SessionMax: max, File: "/etc/gangway/settings"}, Log: record,
		Host: hostOfTests, Network: networkOfTests, Look: lookOfTests})
	return w
}

// What the next world is made with, for the screen. A test that sets one
// puts it back.
var (
	hostOfTests, networkOfTests string
	lookOfTests                 terminal.Look
)

const (
	sessionLength = 30 * time.Minute
	question      = "  run it?   y yes   n no "
)

// The question to extend in the plain look, just shown.
var extendQuestion = "\n  -- time is up " + strings.Repeat("-", 25) + "  answer in 60 s\n\n  extend by 30 min?   y yes   n no "

// extendShown fits the question to extend as the last thing on screen,
// however long it still waits.
var extendShown = regexp.MustCompile(`\n  -- time is up -{25}  answer in \d+ s\n\n  extend by 30 min\?   y yes   n no $`)

// asksToExtend tells whether the question to extend is the last thing on
// screen.
func (w *world) asksToExtend() bool { return extendShown.MatchString(w.screen.String()) }

// questions counts how often the question to extend was drawn.
func (w *world) questions() int {
	return strings.Count(w.screen.String(), "\n  extend by 30 min?   y yes   n no ")
}

// sessionTimer lets the session timer that was started last run out.
func (w *world) sessionTimer() { w.l.SessionTimer(w.stimers[len(w.stimers)-1].number) }

// timeUp lets the session's time run out.
func (w *world) timeUp() {
	w.pass(sessionLength)
	w.sessionTimer()
}

// extending gives a live session with the question to extend on screen
// and PROMPT_MIN_SHOW passed.
func extending(t *testing.T) *world {
	t.Helper()
	w := live(t)
	w.timeUp()
	w.pass(time.Second)
	if w.l.session.State() != session.Extending {
		t.Fatalf("the session is %s, want Extending", w.l.session.State())
	}
	return w
}

func (w *world) sentHas(part string) bool { return strings.Contains(strings.Join(w.sent, "\n"), part) }

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
	return strings.Count(w.screen.String(), "  > echo "+id+"\n\n"+question)
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
	if strings.Contains(w.screen.String(), "run it?") {
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
	want := "\n  -- request 1 " + strings.Repeat("-", 26) + "  30 min left\n\n  > echo a\n\n" + question +
		"\n\n  -- request 1 -- running " + strings.Repeat("-", 15) + "  Ctrl+C stops it\n\n  | a\n\n  + ended, exit 0, 1 line sent to the AI\n"
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
	if !strings.HasSuffix(w.screen.String(), "  > echo a\n\n"+question) {
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
	if w.l.session.State() != session.Ended {
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
	if !strings.Contains(w.screen.String(), "\n  reason    Connection lost\n") {
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
	if strings.HasSuffix(w.screen.String(), question) || w.prompts("a") != 1 {
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

// PIE-05: when the session's clock runs out, the gate removes the prompt
// and the line empties.
func TestPIE05_TheClockRefusesEverythingThatWaits(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.ask("b")
	w.ask("c")
	w.sessionTimer()
	w.wantSent(t, "time_up", "refused b time_up", "refused c time_up", "refused a time_up")
	w.wantStarts(t)
	if w.prompts("b")+w.prompts("c") != 0 || w.prompts("a") != 1 {
		t.Fatal("a prompt was drawn for a request that was refused")
	}
	w.pass(time.Second)
	w.press("y")
	w.pass(time.Second)
	w.press("y")
	w.wantStarts(t)
}

// UNIT-LOOP-15. Requirement: LEN-01, LEN-03.
func TestUNITLOOP15_TimeUpThenExtended(t *testing.T) {
	w := live(t)
	w.timeUp()
	w.wantSent(t, "time_up")
	if !strings.HasSuffix(w.screen.String(), extendQuestion) {
		t.Fatalf("the question is not the last thing on screen: %q", w.screen.String())
	}
	w.pass(time.Second)
	w.press("y")
	w.wantSent(t, "time_up", "extended 1800")
	if !strings.HasSuffix(w.screen.String(), extendQuestion+"\n  + extended by 30 min\n") {
		t.Fatalf("the screen shows %q", w.screen.String())
	}
	w.ask("a")
	if w.prompts("a") != 1 || w.l.State() != Live {
		t.Fatal("no prompt for a request after the session was extended")
	}
}

// UNIT-LOOP-16. Requirement: LEN-06.
func TestUNITLOOP16_RequestsAreRefusedWhileTheTimeIsUp(t *testing.T) {
	w := extending(t)
	w.l.Request(req("a"))
	w.l.Request(request.Request{ID: "bad", Command: "ls\x1b[2J"})
	w.wantSent(t, "time_up", "refused a time_up", "refused bad time_up")
	screen := w.screen.String()
	if strings.Contains(screen, "run it?") || strings.Contains(screen, "echo a") {
		t.Fatalf("a prompt was shown: %q", screen)
	}
	if strings.Count(screen, "\n  x refused a request with no prompt: the session's time is up\n") != 2 {
		t.Fatalf("the screen does not say why, once for each: %q", screen)
	}
	if !w.asksToExtend() {
		t.Fatalf("the question is not the last thing on screen: %q", screen)
	}
	w.wantStarts(t)
}

// UNIT-LOOP-17. Requirement: LEN-07, LEN-03.
func TestUNITLOOP17_AWaitingPromptIsRefusedAndItsYDoesNotExtend(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.sessionTimer()
	w.press("y")
	w.wantSent(t, "time_up", "refused a time_up")
	w.wantStarts(t)
	want := "  > echo a\n\n" + question + "\n  x the session's time is up, nothing ran\n" + extendQuestion
	if !strings.HasSuffix(w.screen.String(), want) {
		t.Fatalf("the screen shows %q, want it to end with %q", w.screen.String(), want)
	}
	if w.l.session.State() != session.Extending {
		t.Fatalf("the session is %s: the y meant for the prompt answered the question", w.l.session.State())
	}
}

// UNIT-LOOP-18. Requirement: LEN-08.
func TestUNITLOOP18_ARunningCommandIsKilledAtItsNextTimeLimit(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.sessionTimer()
	w.wantSent(t, "started a", "time_up")
	if w.stops+w.kills != 0 {
		t.Fatal("the command was killed when the time ran out")
	}
	if strings.Contains(w.screen.String(), "extend by") {
		t.Fatal("the question was shown while the command runs")
	}
	if !strings.Contains(w.screen.String(), "\n  The session's time is up. The command that runs gets no more time.\n") {
		t.Fatalf("the screen does not say that the time is up: %q", w.screen.String())
	}
	w.l.Timer(w.timers[len(w.timers)-1])
	if w.stops != 1 {
		t.Fatalf("the command was asked to stop %d times at its time limit, want 1", w.stops)
	}
	if w.sentHas("still_running") {
		t.Fatalf("the AI was asked to wait or stop: %q", w.sent)
	}
	if strings.Contains(w.screen.String(), "extend by") {
		t.Fatal("the question was shown before the command was gone")
	}
	w.l.Exited(143)
	w.wantSent(t, "started a", "time_up", "ended a killed_time_up 0")
	if !strings.HasSuffix(w.screen.String(), extendQuestion) {
		t.Fatalf("the question is not shown after the command: %q", w.screen.String())
	}
}

// UNIT-LOOP-19. Requirement: LEN-08.
func TestUNITLOOP19_ARunningCommandMayEndByItself(t *testing.T) {
	w := live(t)
	w.ask("a")
	w.press("y")
	w.sessionTimer()
	w.l.Output([]byte("a\n"))
	w.l.Exited(0)
	w.wantSent(t, "started a", "time_up", "output a a\n", "ended a exit 0")
	if w.stops+w.kills != 0 {
		t.Fatal("the command was killed")
	}
	if !strings.HasSuffix(w.screen.String(), "  | a\n\n  + ended, exit 0, 1 line sent to the AI\n"+extendQuestion) {
		t.Fatalf("the question is not shown under the output: %q", w.screen.String())
	}
}

// UNIT-LOOP-20. Requirement: LEN-04, KEY-03.
func TestUNITLOOP20_NotExtendedEndsTheSession(t *testing.T) {
	for name, do := range map[string]func(w *world){
		"n":         func(w *world) { w.press("n") },
		"ctrl_c":    func(w *world) { w.press("\x03") },
		"no answer": func(w *world) { w.pass(time.Minute); w.sessionTimer() },
	} {
		w := extending(t)
		do(w)
		if w.l.State() != Over || w.l.session.State() != session.Ended {
			t.Fatalf("%s: the loop is %v and the session %s", name, w.l.State(), w.l.session.State())
		}
		if fmt.Sprint(w.closed) != "[time_up]" {
			t.Fatalf("%s: closed %q, want [time_up]", name, w.closed)
		}
		w.wantSent(t, "time_up")
		if !strings.Contains(w.screen.String(), "\n  reason    Expired\n") {
			t.Fatalf("%s: the screen does not say why the session ends: %q", name, w.screen.String())
		}
		w.l.Request(req("late"))
		w.wantSent(t, "time_up")
	}
}

// UNIT-LOOP-21. Requirement: LEN-03.
func TestUNITLOOP21_OtherKeysDoNotAnswerTheQuestion(t *testing.T) {
	w := extending(t)
	for _, key := range []string{"Y", "\r", "\x1b[A", "x"} {
		w.press(key)
	}
	if w.l.session.State() != session.Extending || w.l.State() != Live {
		t.Fatalf("the question was answered: the session is %s", w.l.session.State())
	}
	w.wantSent(t, "time_up")
	if !w.asksToExtend() || w.questions() != 1 {
		t.Fatalf("the screen changed: %q", w.screen.String())
	}
}

// UNIT-LOOP-22. Requirement: LEN-09.
func TestUNITLOOP22_NothingFromTheConnectionMovesTheTime(t *testing.T) {
	fromConnection := func(w *world) {
		w.l.Request(req("a"))
		w.l.Wait("a")
		w.l.Stop("a")
		w.l.Cancel("a")
		w.l.Wait("")
		w.l.Stop("extend")
		w.l.Cancel("extend")
	}
	w := live(t)
	fromConnection(w)
	if len(w.stimers) != 1 || w.sentHas("extended") {
		t.Fatalf("in Live a message moved the time: timers %v, sent %q", w.stimers, w.sent)
	}

	w = extending(t)
	timers := len(w.stimers)
	fromConnection(w)
	if len(w.stimers) != timers || w.sentHas("extended") || w.l.session.State() != session.Extending {
		t.Fatalf("at the question a message moved the time: timers %v, sent %q", w.stimers, w.sent)
	}
}

// UNIT-LOOP-23. Requirement: LEN-06.
func TestUNITLOOP23_ALateTimerLetsNoRequestIn(t *testing.T) {
	w := live(t)
	w.pass(sessionLength)
	w.l.Request(req("a"))
	w.wantSent(t, "time_up", "refused a time_up")
	if w.prompts("a") != 0 {
		t.Fatal("a prompt was shown after the end time")
	}
	if !w.asksToExtend() || w.questions() != 1 {
		t.Fatalf("the question is not shown once, as the last thing: %q", w.screen.String())
	}
}

// UNIT-LOOP-24. Requirement: LEN-03.
func TestUNITLOOP24_OldSessionTimersDoNothingAfterAnExtension(t *testing.T) {
	w := extending(t)
	old := append([]sessionTimer(nil), w.stimers...)
	w.press("y")
	for _, timer := range old {
		w.l.SessionTimer(timer.number)
	}
	w.wantSent(t, "time_up", "extended 1800")
	if w.l.session.State() != session.Live {
		t.Fatalf("an old timer changed the session to %s", w.l.session.State())
	}
	w.timeUp()
	w.wantSent(t, "time_up", "extended 1800", "time_up")
	if !w.asksToExtend() || w.questions() != 2 {
		t.Fatalf("the question is not shown a second time: %q", w.screen.String())
	}
}

// UNIT-LOOP-25. Requirement: LEN-01, LEN-02.
func TestUNITLOOP25_TheLengthTheSessionStartsWith(t *testing.T) {
	w := live(t)
	w.l.Begin()
	if len(w.stimers) != 1 || w.stimers[0].d != sessionLength {
		t.Fatalf("with no length asked for the session timers are %v", w.stimers)
	}
	if !strings.Contains(w.screen.String(), "\n  session   30 min, then you are asked to extend\n") {
		t.Fatalf("the screen does not say how long the session lasts: %q", w.screen.String())
	}

	w = newWorldOf(t, nil, 10*time.Minute)
	w.l.Begin()
	if len(w.stimers) != 1 || w.stimers[0].d != 10*time.Minute {
		t.Fatalf("with ten minutes asked for the session timers are %v", w.stimers)
	}
	if !strings.Contains(w.screen.String(), "\n  session   10 min, then you are asked to extend\n") {
		t.Fatalf("the screen does not say how long the session lasts: %q", w.screen.String())
	}
}

// PIE-07: a length above SESSION_MAX reaches the session cut, and the
// terminal says so.
func TestPIE07_TheCutLengthReachesTheSessionAndTheScreen(t *testing.T) {
	w := newWorldMax(t, nil, 45*time.Minute, 20*time.Minute)
	w.l.Begin()
	if !strings.Contains(w.screen.String(), "            45 min is above the maximum: it was cut to 20 min\n            the maximum is set in /etc/gangway/settings\n") {
		t.Fatalf("the terminal does not show that the length was cut: %q", w.screen.String())
	}
	w.pass(20*time.Minute - time.Second)
	w.ask("a")
	if w.prompts("a") != 1 {
		t.Fatal("a request before the maximum got no prompt")
	}
	w.press("n")
	w.l.Request(req("b"))
	if w.prompts("b") != 0 || !w.sentHas("refused b time_up") {
		t.Fatalf("a request at the maximum was not refused: %q", w.sent)
	}
	if w.l.State() != Over {
		t.Fatalf("at the maximum the loop is %v, want Over", w.l.State())
	}
}

// UNIT-LOOP-26. Requirement: SET-01, SET-04.
func TestUNITLOOP26_ALengthAboveTheMaximumIsCutAndTheScreenSaysSo(t *testing.T) {
	const cut = "            50 min is above the maximum: it was cut to 20 min\n            the maximum is set in /etc/gangway/settings\n"
	for name, w := range map[string]*world{
		"asked for at start": newWorldMax(t, nil, 50*time.Minute, 20*time.Minute),
		"the user's file":    newWorldMax(t, func(s *settings.Settings) { s.SessionLength = 50 * time.Minute }, 0, 20*time.Minute),
	} {
		w.l.Begin()
		if len(w.stimers) != 1 || w.stimers[0].d != 20*time.Minute {
			t.Fatalf("%s: the session timers are %v, want one of 20 minutes", name, w.stimers)
		}
		if !strings.Contains(w.screen.String(), cut) {
			t.Fatalf("%s: the screen does not say that the length was cut: %q", name, w.screen.String())
		}
		if !strings.Contains(w.screen.String(), "\n  session   20 min, the maximum: then it ends\n") {
			t.Fatalf("%s: the screen does not say how long the session lasts: %q", name, w.screen.String())
		}
	}

	w := newWorldMax(t, nil, 10*time.Minute, 20*time.Minute)
	w.l.Begin()
	if len(w.stimers) != 1 || w.stimers[0].d != 10*time.Minute {
		t.Fatalf("with ten minutes asked for the session timers are %v", w.stimers)
	}
	if strings.Contains(w.screen.String(), "above the maximum") {
		t.Fatalf("the screen says that a length below the maximum was cut: %q", w.screen.String())
	}
	if !strings.Contains(w.screen.String(), "\n  session   10 min, then you are asked to extend, up to the maximum of 20 min\n") {
		t.Fatalf("the screen does not name the maximum: %q", w.screen.String())
	}

	w = live(t)
	w.l.Begin()
	if strings.Contains(w.screen.String(), "maximum") {
		t.Fatalf("the screen names a maximum with none set: %q", w.screen.String())
	}
}

// UNIT-LOOP-27. Requirement: LEN-05.
func TestUNITLOOP27_AtTheMaximumTheSessionEndsWithNoQuestion(t *testing.T) {
	w := newWorldMax(t, nil, 0, 40*time.Minute)
	w.timeUp()
	w.pass(time.Second)
	if !strings.HasSuffix(w.screen.String(), "  answer in 60 s\n\n  extend by 10 min, up to the maximum?   y yes   n no ") {
		t.Fatalf("the question does not name what is left to the maximum: %q", w.screen.String())
	}
	w.press("y")
	w.wantSent(t, "time_up", "extended 599")
	if !strings.HasSuffix(w.screen.String(), "\n  + extended by 9 min 59 s, up to the maximum: then the session ends\n") {
		t.Fatalf("the screen does not say that the maximum comes next: %q", w.screen.String())
	}
	if got := w.stimers[len(w.stimers)-1].d; got != 599*time.Second {
		t.Fatalf("the new stretch is %v, want what is left to the maximum", got)
	}

	w.pass(599 * time.Second)
	w.sessionTimer()
	if w.l.State() != Over || w.l.session.State() != session.Ended {
		t.Fatalf("at the maximum the loop is %v and the session %s", w.l.State(), w.l.session.State())
	}
	if fmt.Sprint(w.closed) != "[time_up]" {
		t.Fatalf("closed %q, want [time_up]", w.closed)
	}
	if strings.Count(w.screen.String(), "extend by") != 1 {
		t.Fatalf("a question was shown at the maximum: %q", w.screen.String())
	}
	if !strings.Contains(w.screen.String(), "\n  reason    Expired\n") {
		t.Fatalf("the screen does not say why the session ends: %q", w.screen.String())
	}
	w.l.Request(req("late"))
	w.press("y")
	w.wantSent(t, "time_up", "extended 599", "time_up")
}

// UNIT-LOOP-28. Requirement: LEN-05, LEN-08.
func TestUNITLOOP28_AtTheMaximumARunningCommandIsNotKilled(t *testing.T) {
	w := newWorldMax(t, nil, 0, 20*time.Minute)
	w.ask("a")
	w.press("y")
	w.ask("b")
	w.pass(20 * time.Minute)
	w.sessionTimer()
	w.wantSent(t, "started a", "time_up", "refused b time_up")
	if w.stops+w.kills != 0 || w.l.State() != Live {
		t.Fatalf("at the maximum the command was stopped, or the session ended while it ran: %v", w.l.State())
	}
	w.l.Request(req("c"))
	w.wantSent(t, "started a", "time_up", "refused b time_up", "refused c time_up")

	w.l.Exited(0)
	if w.l.State() != Over || fmt.Sprint(w.closed) != "[time_up]" {
		t.Fatalf("after the command the loop is %v, closed %q", w.l.State(), w.closed)
	}
	if strings.Contains(w.screen.String(), "extend by") {
		t.Fatalf("a question was shown at the maximum: %q", w.screen.String())
	}
	w.wantStarts(t, "echo a")
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-LOOP", "loop_test.go")
}

// The real log fits the loop.
var _ Log = (*log.Log)(nil)

// recorded gives a live world with a real log in a scratch folder.
func recorded(t *testing.T, change func(*settings.Settings)) (*world, *log.Log) {
	t.Helper()
	s := settings.Default()
	if change != nil {
		change(&s)
	}
	// The log takes the time from the world, which is made after it.
	var w *world
	record, err := log.Open(t.TempDir(), s.LogFileSize, s.LogFilesKept, worldClock{&w})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(record.Close)
	w = newWorldLogged(t, change, 0, 0, record)
	return w, record
}

type worldClock struct{ w **world }

func (c worldClock) Now() time.Time { return (*c.w).now.UTC() }

func logText(t *testing.T, record *log.Log) string {
	t.Helper()
	b, err := os.ReadFile(record.File())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// PIE-03.
func TestPIE03_TheLogHoldsEveryStepOfEachRequest(t *testing.T) {
	w, record := recorded(t, nil)
	w.l.Request(request.Request{ID: "r1", Command: "bad\x1b[2J"})
	w.ask("r2")
	w.press("n")
	w.ask("r3")
	w.press("y")
	w.l.Output([]byte("one\n"))
	w.l.Output([]byte("two\n"))
	w.l.Exited(0)

	want := "1970-01-01T00:16:40Z r1 request\n" +
		"  > bad\ufffd[2J\n" +
		"1970-01-01T00:16:40Z r1 refused: control_character\n" +
		"1970-01-01T00:16:40Z r2 request\n" +
		"  > echo r2\n" +
		"1970-01-01T00:16:41Z r2 refused: user\n" +
		"1970-01-01T00:16:41Z r3 request\n" +
		"  > echo r3\n" +
		"1970-01-01T00:16:42Z r3 approved\n" +
		"1970-01-01T00:16:42Z r3 ended: exit code 0\n" +
		"  | one\n" +
		"  | two\n"
	if got := logText(t, record); got != want {
		t.Fatalf("the log holds:\n%s\nwant:\n%s", got, want)
	}
	w.wantSent(t, "refused r1 control_character", "refused r2 user", "started r3", "output r3 one\n", "output r3 two\n", "ended r3 exit 0")
}

// UNIT-LOOP-29. Requirement: LOG-02.
func TestUNITLOOP29_TheScreenSaysWhereTheLogIs(t *testing.T) {
	w, record := recorded(t, func(s *settings.Settings) { s.LogFileSize = 1000 })
	w.l.Begin()
	if !strings.Contains(w.screen.String(), "\n  log       "+record.File()+"\n\n  Paste this line to the AI:\n") {
		t.Fatalf("the start does not name the place of the log:\n%s", w.screen.String())
	}
	w.pass(42 * time.Minute)
	w.press("\x03")
	if !strings.HasSuffix(w.screen.String(), "\n  -- session over "+strings.Repeat("-", 23)+"\n\n"+
		"  reason    Ctrl+C\n"+
		"  duration  42 min\n"+
		"  requests  none\n"+
		"  log       "+record.File()+"\n") {
		t.Fatalf("the end does not name the log:\n%s", w.screen.String())
	}
	if !strings.HasSuffix(record.File(), ".txt") {
		t.Fatalf("the log file is %s", record.File())
	}

	// A log of more than one file.
	w, record = recorded(t, func(s *settings.Settings) { s.LogFileSize = 1000 })
	w.l.Begin()
	for i := 0; i < 20; i++ {
		w.l.Request(request.Request{ID: "r", Command: "bad\x1b" + strings.Repeat("x", 100)})
	}
	w.press("\x03")
	if !strings.HasSuffix(w.screen.String(), "  requests  0 run, 20 refused\n"+
		"  log       "+record.File()+"\n            That file holds the last part. The parts before it are in the same folder.\n") {
		t.Fatalf("the end does not tell of the old files:\n%s", w.screen.String())
	}

	// With no log, the screen names none.
	w = live(t)
	w.l.Begin()
	w.press("\x03")
	if strings.Contains(w.screen.String(), "log ") {
		t.Fatalf("the screen names a log:\n%s", w.screen.String())
	}
}

// brokenLog is a log that fails when the test says so.
type brokenLog struct {
	noLog
	err error
}

func (b *brokenLog) Err() error   { return b.err }
func (b *brokenLog) File() string { return "/tmp/gangway-1/session.txt" }

// UNIT-LOOP-30. Requirement: LOG-06.
func TestUNITLOOP30_AFailedLogEndsTheSession(t *testing.T) {
	failed := errors.New("no space left")
	const says = "\n  reason    Log failed\n"

	// A request that cannot be logged goes no further.
	record := &brokenLog{}
	w := newWorldLogged(t, nil, 0, 0, record)
	record.err = failed
	w.ask("r1")
	w.press("y")
	w.wantSent(t, "refused r1 session_ended")
	w.wantStarts(t)
	if w.prompts("r1") != 0 {
		t.Fatal("a prompt was shown for a request that was not logged")
	}
	if w.l.State() != Over || fmt.Sprint(w.closed) != "[user]" {
		t.Fatalf("the loop is %v, closed with %v", w.l.State(), w.closed)
	}
	if !strings.Contains(w.screen.String(), says) ||
		!strings.HasSuffix(w.screen.String(), "  log       /tmp/gangway-1/session.txt\n            The log is not whole: it could not be written.\n") {
		t.Fatalf("the screen:\n%s", w.screen.String())
	}

	// A prompt waits when the log fails: the next event ends the session.
	record = &brokenLog{}
	w = newWorldLogged(t, nil, 0, 0, record)
	w.ask("r1")
	record.err = failed
	w.press("x")
	w.press("y")
	w.wantSent(t, "refused r1 session_ended")
	w.wantStarts(t)
	if w.l.State() != Over {
		t.Fatalf("the loop is %v", w.l.State())
	}

	// A command runs when the log fails: it is stopped, and nothing new runs.
	record = &brokenLog{}
	w = newWorldLogged(t, nil, 0, 0, record)
	w.ask("r1")
	w.press("y")
	w.ask("r2")
	record.err = failed
	w.l.Output([]byte("one\n"))
	w.l.Timer(-1)
	if w.l.State() != Ending || w.stops != 1 {
		t.Fatalf("the loop is %v and the command was asked to stop %d times", w.l.State(), w.stops)
	}
	w.l.Exited(1)
	if w.l.State() != Over {
		t.Fatalf("the loop is %v", w.l.State())
	}
	w.wantStarts(t, "echo r1")
	if !w.sentHas("refused r2 session_ended") || !w.sentHas("ended r1 killed_session_ended") {
		t.Fatalf("sent %q", w.sent)
	}

	// A log that fails at the start.
	record = &brokenLog{err: failed}
	w = newWorldLogged(t, nil, 0, 0, record)
	w.l.Begin()
	if w.l.State() != Over || !strings.Contains(w.screen.String(), says) {
		t.Fatalf("the loop is %v:\n%s", w.l.State(), w.screen.String())
	}
}

// UNIT-LOOP-32. Requirement: SHOW-01, SHOW-06, KEY-03.
func TestUNITLOOP32_TheStartAndTheEndOfASessionOnTheScreen(t *testing.T) {
	hostOfTests, networkOfTests, lookOfTests = "build-box\x1b[2J", "connected over Tailscale", terminal.Look{Unicode: true}
	defer func() { hostOfTests, networkOfTests, lookOfTests = "", "", terminal.Look{} }()
	w := live(t)
	w.l.Begin()
	start := "\n" +
		"  host      build-box�[2J\n" +
		"  session   30 min, then you are asked to extend\n" +
		"  network   connected over Tailscale\n" +
		"\n  Paste this line to the AI:\n\n/gangway " + w.l.session.Address() + " " + w.l.session.Key() + "\n"
	if w.screen.String() != start {
		t.Fatalf("the start shows:\n%s\nwant:\n%s", w.screen.String(), start)
	}
	if strings.Count(w.screen.String(), w.l.session.Key()) != 1 {
		t.Fatal("the key shows more than once")
	}

	// One runs, one is refused by n, one with no prompt, one is cut off by the end.
	w.pass(10 * time.Minute)
	w.ask("a")
	if !strings.Contains(w.screen.String(), "\n  ── request 1 "+strings.Repeat("─", 26)+"  20 min left\n") {
		t.Fatalf("the header of the first prompt:\n%s", w.screen.String())
	}
	w.press("y")
	w.l.Output([]byte("a\n"))
	w.l.Exited(0)
	w.ask("b")
	w.press("n")
	w.l.Request(request.Request{ID: "bad", Command: "ls\x1b[2J"})
	w.ask("c")
	if !strings.Contains(w.screen.String(), "\n  ── request 3 ─") || strings.Contains(w.screen.String(), "request 4") {
		t.Fatalf("the third prompt is not request 3:\n%s", w.screen.String())
	}
	w.press("y")
	w.ask("d")
	w.screen.Reset()
	w.pass(2*time.Minute + 30*time.Second)
	w.press("\x03\x03")
	if got, want := w.screen.String(), "  The session ends: you pressed Ctrl+C twice. The command is being stopped.\n"; got != want {
		t.Fatalf("while the command is stopped the screen shows %q, want %q", got, want)
	}
	w.screen.Reset()
	w.l.Exited(143)
	end := "\n  ✗ killed: you pressed Ctrl+C\n" +
		"\n  ── session over " + strings.Repeat("─", 23) + "\n\n" +
		"  reason    Ctrl+C\n" +
		"  duration  12 min\n" +
		"  requests  2 run, 3 refused\n"
	if w.screen.String() != end {
		t.Fatalf("the end shows:\n%s\nwant:\n%s", w.screen.String(), end)
	}
	if strings.ContainsRune(w.screen.String(), 0x1b) {
		t.Fatal("an Escape reached the screen")
	}
}

// UNIT-LOOP-31. Requirement: LOG-01, KEY-03.
func TestUNITLOOP31_TheLogHoldsTheStepsOfTheSessionAndNotItsKey(t *testing.T) {
	w, record := recorded(t, nil)
	w.l.Begin()
	w.ask("r1")
	w.timeUp()
	w.pass(time.Second)
	w.press("y")
	w.press("\x03")

	want := "1970-01-01T00:16:40Z session started: it lasts 30 minutes\n" +
		"1970-01-01T00:16:40Z r1 request\n" +
		"  > echo r1\n" +
		"1970-01-01T00:46:41Z session time is up\n" +
		"1970-01-01T00:46:41Z r1 refused: time_up\n" +
		"1970-01-01T00:46:42Z session extended: 30 minutes left\n" +
		"1970-01-01T00:46:42Z session ended: user. Ctrl+C: the session ends.\n"
	if got := logText(t, record); got != want {
		t.Fatalf("the log holds:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(logText(t, record), w.l.session.Key()) || strings.Contains(logText(t, record), "gw_") {
		t.Fatal("the log holds the session key")
	}
}
