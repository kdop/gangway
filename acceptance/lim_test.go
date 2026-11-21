package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// limWorld is a world with short limits, so that a test does not wait long.
// Each of more is one more line of the settings file, and wins over the
// short values.
func limWorld(t *testing.T, more ...string) *world {
	short := []string{"COMMAND_TIME_LIMIT = 1s", "QUESTION_WAIT = 1s", "KILL_WAIT = 500ms", "DOUBLE_CTRL_C_WINDOW = 1s"}
	var settings []string
	for _, s := range short {
		if !hasSetting(more, strings.Fields(s)[0]) {
			settings = append(settings, s)
		}
	}
	return newWorld(t, append(settings, more...)...)
}

// start asks for a command and presses y. The call stays open.
func (w *world) start(command string) *asked {
	w.t.Helper()
	n := w.prompts()
	a := w.ask(command)
	w.prompt(n + 1)
	w.press("y")
	return a
}

// pid waits for a file that holds a process ID and gives the ID. The
// program is killed when the test is over.
func (w *world) pid(name string) int {
	w.t.Helper()
	var pid int
	w.until("the file "+name+" holds a process ID", func() bool {
		b, err := os.ReadFile(w.file(name))
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && pid > 0
	})
	w.t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

func runs(pid int) bool { return syscall.Kill(pid, 0) == nil }

// stillRuns fails the test if the program ends within a short time.
func (w *world) stillRuns(pid int) {
	w.t.Helper()
	w.stays("the program "+strconv.Itoa(pid)+" runs", func() bool { return runs(pid) })
}

// post makes one API call that sends nothing.
func (w *world) post(path string) answer {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), soon)
	defer cancel()
	got, err := w.call(ctx, "POST", path, "")
	if err != nil {
		w.t.Fatalf("POST %s: %v", path, err)
	}
	return got
}

// asksWaitOrStop fails the test unless the answer is "still running" with
// the question: an ID to answer with, and the time to answer in.
func (got answer) asksWaitOrStop(t *testing.T) {
	t.Helper()
	var body struct {
		Seconds int `json:"answer_within_seconds"`
	}
	json.Unmarshal([]byte(got.Body), &body)
	if got.Status != 200 || got.State != "running" || got.ID == "" || body.Seconds <= 0 {
		t.Fatalf("want running with an id and the time to answer in, got %d %s", got.Status, got.Body)
	}
}

// killed fails the test unless the answer says the command was killed
// this way.
func (got answer) killed(t *testing.T, how string) {
	t.Helper()
	if got.Status != 200 || got.State != "ended" || got.How != how {
		t.Fatalf("want ended with %s, got %d %s", how, got.Status, got.Body)
	}
}

// limits gives the limits that hello reports, in seconds.
func (w *world) limits() (command, answer int) {
	w.t.Helper()
	var body struct {
		Limits struct {
			Command int `json:"command_seconds"`
			Answer  int `json:"answer_seconds"`
		} `json:"limits"`
	}
	got := w.post("/v1/hello")
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil || got.Status != 200 {
		w.t.Fatalf("hello: %d %s", got.Status, got.Body)
	}
	return body.Limits.Command, body.Limits.Answer
}

// A shell that writes its process ID to a file and then runs long.
func long(w *world, name string) string {
	return "echo $$ > " + w.file(name) + "; sleep 300"
}

func TestLIM01_1_PastTheTimeLimitTheAIIsAsked(t *testing.T) {
	w := limWorld(t, "QUESTION_WAIT = 10s")
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	a.noAnswerYet()
	a.answer().asksWaitOrStop(t)
	w.stillRuns(pid)
}

func TestLIM01_2_AShortCommandGetsNoQuestion(t *testing.T) {
	w := limWorld(t)
	got := w.run("echo ok")
	got.ran(t, 0)
	if got.Output != "ok\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
}

func TestLIM02_1_StopKillsTheCommand(t *testing.T) {
	w := limWorld(t, "QUESTION_WAIT = 10s")
	a := w.start("echo so far; " + long(w, "pid"))
	pid := w.pid("pid")
	first := a.answer()
	first.asksWaitOrStop(t)
	got := w.post("/v1/requests/" + first.ID + "/stop")
	got.killed(t, "killed_by_ai")
	if out := first.Output + got.Output; out != "so far\n" {
		t.Fatalf("want the output so far, got %q", out)
	}
	w.gone(pid)
}

func TestLIM02_2_WaitLetsItRunAndTheQuestionComesAgain(t *testing.T) {
	w := limWorld(t, "QUESTION_WAIT = 10s")
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	first := a.answer()
	first.asksWaitOrStop(t)

	asked := time.Now()
	second := w.post("/v1/requests/" + first.ID + "/wait")
	second.asksWaitOrStop(t)
	if took := time.Since(asked); took < 900*time.Millisecond {
		t.Fatalf("the question came again after %s, before another time limit", took)
	}
	w.stillRuns(pid)
}

func TestLIM02_3_AfterWaitTheWholeResultComes(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 2s", "QUESTION_WAIT = 10s")
	first := w.run("echo one; sleep 3; echo two; exit 4")
	first.asksWaitOrStop(t)
	got := w.post("/v1/requests/" + first.ID + "/wait")
	got.ran(t, 4)
	if out := first.Output + got.Output; out != "one\ntwo\n" {
		t.Fatalf("want the whole output, got %q", out)
	}
}

func TestLIM03_1_NoAnswerKillsTheCommand(t *testing.T) {
	w := limWorld(t)
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	first := a.answer()
	first.asksWaitOrStop(t)
	if !runs(pid) {
		t.Fatal("the command was killed before the question could be answered")
	}
	w.gone(pid)
	w.shows("Killed: the time limit passed and the AI gave no answer.")
	w.post("/v1/requests/"+first.ID+"/wait").killed(t, "killed_no_answer")
}

func TestLIM04_1_ACommandThatEndsWhileTheQuestionWaits(t *testing.T) {
	for _, next := range []string{"wait", "stop"} {
		w := limWorld(t, "QUESTION_WAIT = 10s")
		first := w.run("echo one; sleep 2; echo two; touch " + w.file("end"))
		first.asksWaitOrStop(t)
		w.shows("Done. Exit code 0.")

		got := w.post("/v1/requests/" + first.ID + "/" + next)
		got.ran(t, 0)
		if out := first.Output + got.Output; out != "one\ntwo\n" {
			t.Fatalf("%s: want the whole output, got %q", next, out)
		}
		if !w.exists("end") || strings.Contains(w.screen.text(), "Killed") {
			t.Fatalf("%s: the command was killed", next)
		}
	}
}

// The starting values are a minute each, too long to wait for: the test
// reads them from what hello reports. TestLIM06_2 shows that the limits
// act at the values they are set to.
func TestLIM06_1_TheLimitsStartAtTheirStartingValues(t *testing.T) {
	w := newWorld(t)
	if command, answer := w.limits(); command != 60 || answer != 60 {
		t.Fatalf("want 60 and 60 seconds, got %d and %d", command, answer)
	}
}

func TestLIM06_2_TheLimitsActAtTheSetValues(t *testing.T) {
	const timeLimit, questionWait = 2 * time.Second, 3 * time.Second
	w := newWorld(t, "COMMAND_TIME_LIMIT = 2s", "QUESTION_WAIT = 3s", "KILL_WAIT = 500ms")
	if command, answer := w.limits(); command != 2 || answer != 3 {
		t.Fatalf("want 2 and 3 seconds, got %d and %d", command, answer)
	}

	a := w.ask(long(w, "pid"))
	w.prompt(1)
	approved := time.Now()
	w.press("y")
	pid := w.pid("pid")
	a.answer().asksWaitOrStop(t)
	asked := time.Now()
	if took := asked.Sub(approved); took < timeLimit {
		t.Fatalf("the question came after %s, want %s", took, timeLimit)
	}

	// The clock of the test starts a moment after that of the cli-tool.
	const slack = 300 * time.Millisecond
	w.gone(pid)
	if took := time.Since(asked); took < questionWait-slack {
		t.Fatalf("the command was killed after %s, want %s", took, questionWait)
	}
}

func TestLIM07_1_TheAIHangsUpAndTheCommandIsKilled(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s")
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	a.hangUp()
	w.gone(pid)
	w.shows("Killed: the AI hung up.")
	w.live()
}

func TestLIM08_1_CtrlCKillsTheRunningCommand(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s")
	a := w.start(long(w, "pid"))
	pid := w.pid("pid")
	w.press("\x03")
	a.answer().killed(t, "killed_by_user")
	w.gone(pid)
	w.live()
}

func TestLIM08_2_AfterCtrlCOnACommandANewRequestIsAsked(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s")
	a := w.start(long(w, "pid"))
	w.pid("pid")
	w.press("\x03")
	a.answer().killed(t, "killed_by_user")

	w.run("touch "+w.file("ran")).ran(t, 0)
	if w.prompts() != 2 || !w.exists("ran") {
		t.Fatal("the new request was not asked and run")
	}
}

func TestLIM09_1_CtrlCRefusesTheWaitingPrompt(t *testing.T) {
	w := limWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.press("\x03")
	a.answer().refused(t, "user")
	w.notRun("ran")
	w.live()
}

func TestLIM09_2_CtrlCRefusesOnlyTheFirstOfTwo(t *testing.T) {
	w := limWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("\x03")
	a.answer().refused(t, "user")
	w.prompt(2)
	w.shows("  > touch " + w.file("b") + "\n" + question)
	b.noAnswerYet()
	w.notRun("a")
	w.notRun("b")
}

func TestLIM10_1_TwoCtrlCAtAPromptEndTheCliTool(t *testing.T) {
	w := limWorld(t)
	w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.press("\x03")
	time.Sleep(100 * time.Millisecond)
	w.press("\x03")
	w.ended()
	w.notRun("ran")
}

func TestLIM10_2_TwoCtrlCOnACommandEndTheCliTool(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s")
	w.start(long(w, "pid"))
	pid := w.pid("pid")
	w.press("\x03")
	time.Sleep(100 * time.Millisecond)
	w.press("\x03")
	w.ended()
	w.gone(pid)
}

func TestLIM10_3_ANewRequestBetweenTwoCtrlCDoesNotSaveTheCliTool(t *testing.T) {
	w := limWorld(t, "DOUBLE_CTRL_C_WINDOW = 5s")
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	w.press("\x03")
	a.answer().refused(t, "user")
	w.ask("touch " + w.file("b"))
	w.promptShown(2)
	w.press("\x03")
	w.ended()
	w.notRun("a")
	w.notRun("b")
}

func TestLIM10_4_TwoSlowCtrlCRefuseOneRequestEach(t *testing.T) {
	const window = 300 * time.Millisecond
	w := limWorld(t, "DOUBLE_CTRL_C_WINDOW = "+window.String())
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("\x03")
	a.answer().refused(t, "user")
	w.prompt(2)
	time.Sleep(2 * window)
	w.press("\x03")
	b.answer().refused(t, "user")
	w.notRun("a")
	w.notRun("b")
	w.stays("the cli-tool runs", func() bool {
		select {
		case <-w.cliDone:
			return false
		default:
			return true
		}
	})
	w.live()
}

func TestLIM11_1_BothEndsOfAPipeAreKilled(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s")
	end := func(name string) string { return "sh -c 'echo $$ > " + w.file(name) + "; exec sleep 300'" }
	a := w.start(end("left") + " | " + end("right"))
	left, right := w.pid("left"), w.pid("right")
	w.press("\x03")
	a.answer().killed(t, "killed_by_user")
	w.gone(left)
	w.gone(right)
}

func TestLIM11_2_AProgramInTheBackgroundIsKilledWithItsCommand(t *testing.T) {
	w := limWorld(t, "QUESTION_WAIT = 10s")
	a := w.start("sleep 300 & echo $! > " + w.file("pid") + "; wait")
	pid := w.pid("pid")
	first := a.answer()
	first.asksWaitOrStop(t)
	w.post("/v1/requests/"+first.ID+"/stop").killed(t, "killed_by_ai")
	w.gone(pid)
}

// A command that leaves a long program in the background and ends.
func leavesOne(w *world) string {
	return "nohup sleep 300 > " + w.file("out") + " 2>&1 & echo $! > " + w.file("pid")
}

func TestLIM12_1_AProgramLeftInTheBackgroundOutlivesItsCommand(t *testing.T) {
	w := limWorld(t)
	asked := time.Now()
	w.run(leavesOne(w)).ran(t, 0)
	// "At once" is: before the time limit, less the wait for the prompt.
	if took := time.Since(asked) - 2*promptMinShow; took >= time.Second {
		t.Fatalf("the result came after %s, not at once", took)
	}
	w.stillRuns(w.pid("pid"))
}

func TestLIM12_2_AfterSuchACommandANewRequestIsAsked(t *testing.T) {
	w := limWorld(t)
	w.run(leavesOne(w)).ran(t, 0)
	pid := w.pid("pid")
	w.run("touch "+w.file("ran")).ran(t, 0)
	if w.prompts() != 2 || !w.exists("ran") {
		t.Fatal("the new request was not asked and run")
	}
	w.stillRuns(pid)
}

func TestLIM13_1_ACommandIsAskedToStopFirst(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s", "KILL_WAIT = 5s")
	a := w.start("trap 'echo stopped > " + w.file("mark") + "; exit 0' TERM; echo $$ > " + w.file("pid") + "; while :; do sleep 0.1; done")
	pid := w.pid("pid")
	w.press("\x03")
	a.answer().killed(t, "killed_by_user")
	if !w.exists("mark") {
		t.Fatal("the command was not asked to stop: it left no mark")
	}
	w.gone(pid)
}

func TestLIM13_2_ACommandThatDoesNotStopIsKilledByForce(t *testing.T) {
	w := limWorld(t, "COMMAND_TIME_LIMIT = 30s", "KILL_WAIT = 1s")
	a := w.start("trap '' TERM; echo $$ > " + w.file("pid") + "; while :; do sleep 0.1; done")
	pid := w.pid("pid")
	w.press("\x03")
	w.stillRuns(pid)
	a.answer().killed(t, "killed_by_user")
	w.gone(pid)
}
