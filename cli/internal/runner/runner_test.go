package runner

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/shared/designcheck"
)

// The gate and the runner fit each other.
var (
	_ gate.Runner = (*Runner)(nil)
	_ Gate        = (*gate.Gate)(nil)
)

var settings = Settings{Shell: "/bin/sh", TimeLimit: 60 * time.Second, QuestionWait: 30 * time.Second, KillWait: 2 * time.Second}

var reqA = request.Request{ID: "a", Command: "grep x file | tail -n 1"}

// approver is a real gate with fake neighbours. It is the only way to get
// an approval.
type approver struct {
	queue []request.Request
	got   *gate.Approval
}

func (a *approver) Next() (request.Request, bool) {
	if len(a.queue) == 0 {
		return request.Request{}, false
	}
	r := a.queue[0]
	a.queue = a.queue[1:]
	return r, true
}
func (a *approver) Prompt(request.Request)              {}
func (a *approver) Answer(request.Request, gate.Answer) {}
func (a *approver) Started(string)                      {}
func (a *approver) Refused(string, string)              {}
func (a *approver) Run(ap *gate.Approval)               { a.got = ap }
func (a *approver) Now() time.Time                      { return time.Unix(1000, 0) }

func approve(req request.Request) *gate.Approval {
	a := &approver{queue: []request.Request{req}}
	g := gate.New(a, a, a, a, a, 0)
	g.Ready()
	g.Key([]byte("y"))
	return a.got
}

type timerStart struct {
	d      time.Duration
	number int
}

type started struct{ shell, command string }

// world is every neighbour of the runner, faked. log holds what the screen
// and the connection were asked, in order.
type world struct {
	r *Runner

	startErr error
	starts   []started
	stops    int
	kills    int
	timers   []timerStart
	log      []string
	results  []Result
	dones    int
}

func (w *world) Start(shell, command string) (Process, error) {
	if w.startErr != nil {
		return nil, w.startErr
	}
	w.starts = append(w.starts, started{shell, command})
	return w, nil
}
func (w *world) Stop() { w.stops++ }
func (w *world) Kill() { w.kills++ }

type fakeTimers struct{ w *world }

func (f fakeTimers) Start(d time.Duration, number int) {
	f.w.timers = append(f.w.timers, timerStart{d, number})
}

type fakeScreen struct{ w *world }

func (f fakeScreen) Output(text string) { f.w.log = append(f.w.log, "screen:"+text) }
func (f fakeScreen) Ended(req request.Request, res Result) {
	f.w.log = append(f.w.log, "screen ended:"+req.ID+":"+res.How)
}

type fakeSender struct{ w *world }

func (f fakeSender) Output(id, text string) { f.w.log = append(f.w.log, "send:"+id+":"+text) }
func (f fakeSender) StillRunning(id string) { f.w.log = append(f.w.log, "still_running:"+id) }
func (f fakeSender) Ended(id string, res Result) {
	f.w.log = append(f.w.log, "ended:"+id+":"+res.How)
	f.w.results = append(f.w.results, res)
}

type fakeGate struct{ w *world }

func (f fakeGate) Done() { f.w.dones++ }

func idle() *world {
	w := &world{}
	w.r = New(w, fakeTimers{w}, fakeScreen{w}, fakeSender{w}, fakeGate{w}, settings)
	return w
}

func running() *world {
	w := idle()
	w.r.Run(approve(reqA))
	return w
}

// lastTimer fires the timer the runner waits for.
func (w *world) lastTimer() { w.r.Timer(w.timers[len(w.timers)-1].number) }

func asked() *world {
	w := running()
	w.lastTimer()
	return w
}

func (w *world) count(prefix string) int {
	n := 0
	for _, l := range w.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func wantState(t *testing.T, w *world, want State) {
	t.Helper()
	if got := w.r.State(); got != want {
		t.Fatalf("state is %v, want %v", got, want)
	}
}

// wantEnded checks that exactly one ended was sent, with this way, and that
// the gate got done once.
func wantEnded(t *testing.T, w *world, how string) Result {
	t.Helper()
	if len(w.results) != 1 || w.results[0].How != how {
		t.Fatalf("results are %+v, want one with %s", w.results, how)
	}
	if w.dones != 1 {
		t.Fatalf("the gate got done %d times, want 1", w.dones)
	}
	wantState(t, w, Idle)
	return w.results[0]
}

func wantLastTimer(t *testing.T, w *world, d time.Duration) {
	t.Helper()
	if len(w.timers) == 0 || w.timers[len(w.timers)-1].d != d {
		t.Fatalf("timers are %v, want the last to be %v", w.timers, d)
	}
}

// UNIT-RUNNER-01. Requirement: GATE-01, GATE-11.
func TestUNITRUNNER01_RunStartsTheExactCommand(t *testing.T) {
	w := running()
	wantState(t, w, Running)
	if len(w.starts) != 1 || w.starts[0] != (started{"/bin/sh", reqA.Command}) {
		t.Fatalf("starts are %v", w.starts)
	}
	wantLastTimer(t, w, settings.TimeLimit)
}

// UNIT-RUNNER-02. Requirement: GATE-15.
func TestUNITRUNNER02_AUsedApprovalRunsNothing(t *testing.T) {
	w := idle()
	used := approve(reqA)
	used.Use()
	w.r.Run(used)
	w.r.Run(nil)
	w.r.Run(&gate.Approval{})
	wantState(t, w, Idle)
	if len(w.starts)+len(w.log)+len(w.timers)+w.dones != 0 {
		t.Fatalf("something happened: starts %v, log %v", w.starts, w.log)
	}
}

// UNIT-RUNNER-03. Requirement: GATE-06.
func TestUNITRUNNER03_OneCommandAtATime(t *testing.T) {
	w := running()
	second := approve(request.Request{ID: "b", Command: "echo b"})
	w.r.Run(second)
	if len(w.starts) != 1 {
		t.Fatalf("%d commands were started, want 1", len(w.starts))
	}
	if _, ok := second.Use(); !ok {
		t.Fatal("the second approval was used up")
	}
}

// UNIT-RUNNER-04. Requirement: SHOW-02.
func TestUNITRUNNER04_OutputGoesToTheScreenAndTheConnection(t *testing.T) {
	w := running()
	w.r.Output([]byte("one\n"))
	w.r.Output([]byte("two\n"))
	want := "[screen:one\n send:a:one\n screen:two\n send:a:two\n]"
	if got := fmt.Sprint(w.log); got != want {
		t.Fatalf("log is %q, want %q", got, want)
	}
}

// UNIT-RUNNER-05. Requirement: LIM-01.
func TestUNITRUNNER05_ExitBeforeTheTimeLimit(t *testing.T) {
	w := running()
	w.r.Exited(3)
	if res := wantEnded(t, w, HowExit); res.ExitCode != 3 {
		t.Fatalf("exit code is %d, want 3", res.ExitCode)
	}
	if w.count("still_running") != 0 {
		t.Fatal("still_running was sent")
	}
}

// UNIT-RUNNER-06. Requirement: LIM-01.
func TestUNITRUNNER06_TheTimeLimitAsks(t *testing.T) {
	w := running()
	w.lastTimer()
	wantState(t, w, Asked)
	if w.count("still_running:a") != 1 {
		t.Fatalf("log is %v, want one still_running", w.log)
	}
	wantLastTimer(t, w, settings.QuestionWait)
	if w.stops+w.kills != 0 {
		t.Fatal("the command was killed")
	}
}

// UNIT-RUNNER-07. Requirement: LIM-02.
func TestUNITRUNNER07_WaitRunsOn(t *testing.T) {
	w := asked()
	w.r.Wait()
	wantState(t, w, Running)
	wantLastTimer(t, w, settings.TimeLimit)
	w.lastTimer()
	wantState(t, w, Asked)
	if w.count("still_running:a") != 2 {
		t.Fatalf("log is %v, want two still_running", w.log)
	}
	if w.stops+w.kills != 0 {
		t.Fatal("the command was killed")
	}
}

// UNIT-RUNNER-08. Requirement: LIM-02.
func TestUNITRUNNER08_StopKills(t *testing.T) {
	w := asked()
	w.r.Stop()
	wantState(t, w, Stopping)
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledByAI)
}

// UNIT-RUNNER-09. Requirement: LIM-03.
func TestUNITRUNNER09_NoAnswerKills(t *testing.T) {
	w := asked()
	w.lastTimer()
	wantState(t, w, Stopping)
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledNoAnswer)
}

// UNIT-RUNNER-10. Requirement: LIM-04.
func TestUNITRUNNER10_ExitWhileTheQuestionWaits(t *testing.T) {
	w := asked()
	w.r.Exited(0)
	if res := wantEnded(t, w, HowExit); res.ExitCode != 0 {
		t.Fatalf("exit code is %d, want 0", res.ExitCode)
	}
	if w.stops+w.kills != 0 {
		t.Fatal("something was killed")
	}
}

// UNIT-RUNNER-11. Requirement: LIM-08.
func TestUNITRUNNER11_CtrlCKills(t *testing.T) {
	w := running()
	w.r.CtrlC()
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledByUser)
}

// UNIT-RUNNER-12. Requirement: LIM-07.
func TestUNITRUNNER12_CancelKillsItsOwnOnly(t *testing.T) {
	w := running()
	w.r.Cancel("zz")
	wantState(t, w, Running)
	if w.stops != 0 {
		t.Fatal("a cancel for another request stopped the command")
	}
	w.r.Cancel("a")
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledAIGone)
}

// UNIT-RUNNER-13. Requirement: SES-05.
func TestUNITRUNNER13_TheEndOfTheSessionKills(t *testing.T) {
	for _, start := range []func() *world{running, asked} {
		w := start()
		w.r.End()
		if w.stops != 1 {
			t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
		}
		w.r.Exited(143)
		wantEnded(t, w, HowKilledSessionEnded)
	}
}

// UNIT-RUNNER-14. Requirement: LIM-02, LIM-03.
func TestUNITRUNNER14_AnOldTimerDoesNothing(t *testing.T) {
	w := asked()
	question := w.timers[len(w.timers)-1].number
	w.r.Wait()
	w.r.Timer(question)
	wantState(t, w, Running)
	if w.stops+w.kills != 0 {
		t.Fatal("the old question timer killed the command")
	}
	w.r.Exited(0)
	before := len(w.log)
	for _, tm := range w.timers {
		w.r.Timer(tm.number)
	}
	wantState(t, w, Idle)
	if len(w.log) != before || w.stops+w.kills != 0 {
		t.Fatal("a timer did something after the command ended")
	}

	// A new command must not be hit by a timer of the old one.
	w.r.Run(approve(reqA))
	for _, tm := range w.timers[:len(w.timers)-1] {
		w.r.Timer(tm.number)
	}
	wantState(t, w, Running)
}

// UNIT-RUNNER-15. Requirement: LIM-13.
func TestUNITRUNNER15_ForceAfterTheKillWait(t *testing.T) {
	w := running()
	w.r.CtrlC()
	wantLastTimer(t, w, settings.KillWait)
	if w.kills != 0 {
		t.Fatal("killed by force before the wait")
	}
	w.lastTimer()
	if w.kills != 1 {
		t.Fatalf("killed by force %d times, want 1", w.kills)
	}
	wantState(t, w, Stopping)
	w.r.Exited(137)
	wantEnded(t, w, HowKilledByUser)
}

// UNIT-RUNNER-16. Requirement: LIM-08.
func TestUNITRUNNER16_TheFirstReasonWins(t *testing.T) {
	w := running()
	w.r.CtrlC()
	w.r.Stop()
	w.r.Cancel("a")
	w.r.End()
	w.r.CtrlC()
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledByUser)
}

// UNIT-RUNNER-17. Requirement: LIM-02.
func TestUNITRUNNER17_OutputWhileStopping(t *testing.T) {
	w := asked()
	w.r.Stop()
	w.r.Output([]byte("last words\n"))
	w.r.Exited(143)
	got := fmt.Sprint(w.log[len(w.log)-4:])
	want := "[screen:last words\n send:a:last words\n screen ended:a:killed_by_ai ended:a:killed_by_ai]"
	if got != want {
		t.Fatalf("log ends with %q, want %q", got, want)
	}
}

// UNIT-RUNNER-18. Requirement: SHOW-03.
func TestUNITRUNNER18_TheAIGetsText(t *testing.T) {
	w := running()
	w.r.Output([]byte("a\xffb"))
	alpha := []byte("α")
	w.r.Output(append([]byte("x"), alpha[0]))
	w.r.Output(append([]byte{alpha[1]}, 'y'))
	w.r.Output(append([]byte("z"), alpha[0]))
	w.r.Exited(0)
	sent := ""
	for _, l := range w.log {
		if s, ok := strings.CutPrefix(l, "send:a:"); ok {
			sent += s
		}
	}
	if want := "a�bxαyz�"; sent != want {
		t.Fatalf("sent %q, want %q", sent, want)
	}
	if !wantEnded(t, w, HowExit).BytesReplaced {
		t.Fatal("ended does not say bytes_replaced")
	}

	clean := running()
	clean.r.Output([]byte("καλημέρα\n"))
	clean.r.Exited(0)
	if wantEnded(t, clean, HowExit).BytesReplaced {
		t.Fatal("ended says bytes_replaced for clean output")
	}
}

// UNIT-RUNNER-19. Requirement: GATE-11.
func TestUNITRUNNER19_AFailedStart(t *testing.T) {
	w := idle()
	w.startErr = errors.New("no such file")
	w.r.Run(approve(reqA))
	wantEnded(t, w, HowStartFailed)
	if w.count("send:a:") != 1 || !strings.Contains(strings.Join(w.log, ""), "no such file") {
		t.Fatalf("the reason was not sent: %v", w.log)
	}
	if len(w.timers) != 0 {
		t.Fatal("a timer was started")
	}
}

// UNIT-RUNNER-20. Requirement: GATE-01, LIM-02.
func TestUNITRUNNER20_IdleDropsEverythingAndStopWorksWhileRunning(t *testing.T) {
	w := idle()
	w.r.Output([]byte("x"))
	w.r.Exited(0)
	w.r.Timer(0)
	w.r.Timer(1)
	w.r.Wait()
	w.r.Stop()
	w.r.CtrlC()
	w.r.Cancel("")
	w.r.Cancel("a")
	w.r.End()
	wantState(t, w, Idle)
	if len(w.starts)+len(w.log)+len(w.timers)+w.stops+w.kills+w.dones != 0 {
		t.Fatalf("something happened in Idle: %v", w.log)
	}

	w = running()
	w.r.Wait()
	wantState(t, w, Running)
	w.r.Stop()
	if w.stops != 1 {
		t.Fatalf("the program was asked to stop %d times, want 1", w.stops)
	}
	w.r.Exited(143)
	wantEnded(t, w, HowKilledByAI)
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-RUNNER", "runner_test.go", "starter_test.go")
}
