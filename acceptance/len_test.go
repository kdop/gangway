package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The tests of the session's length run with lengths of a few seconds, set
// in the settings file or asked for at start. "Just before" and "not
// before" are checked with a margin, because the test sees the start of
// the session a moment late and its own clock can be held up.
const margin = time.Second

// extendQuestion is what the terminal shows when the time is up.
func extendQuestion(length string) string {
	return "\nThe session's time is up.\nExtend it by " + length + "? y/n "
}

// questions counts how often the question to extend was shown.
func (w *world) questions() int {
	return strings.Count("\n"+w.screen.text(), "\nExtend it by ")
}

// question waits until n questions to extend were shown, and until a key
// counts.
func (w *world) question(n int) {
	w.t.Helper()
	w.until(fmt.Sprintf("question %d to extend shows", n), func() bool { return w.questions() >= n })
	if got := w.questions(); got != n {
		w.t.Fatalf("want %d questions to extend on the screen, got %d", n, got)
	}
	time.Sleep(2 * promptMinShow)
}

// noQuestionBefore fails the test if question n+1 shows before this moment.
func (w *world) noQuestionBefore(at time.Time, n int) {
	w.t.Helper()
	for time.Now().Before(at) {
		if got := w.questions(); got != n {
			w.t.Fatalf("question %d to extend was shown %s too early", got, time.Until(at).Round(time.Millisecond))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sessionTime is what the API tells about the session's time.
type sessionTime struct {
	SecondsLeft int  `json:"seconds_left"`
	TimeUp      bool `json:"time_up"`
	Requests    struct {
		Running *string `json:"running"`
	} `json:"requests"`
}

func (w *world) sessionTime() sessionTime {
	w.t.Helper()
	got, err := w.call(context.Background(), "GET", "/v1/session", "")
	var body sessionTime
	if err != nil || got.Status != 200 || json.Unmarshal([]byte(got.Body), &body) != nil {
		w.t.Fatalf("session: %d %s %v", got.Status, got.Body, err)
	}
	return body
}

// dead fails the test unless the key is refused and the cli-tool has
// ended.
func (w *world) dead() {
	w.t.Helper()
	w.ended()
	w.until("the key is refused", func() bool {
		got, err := w.call(context.Background(), "GET", "/v1/session", "")
		return err == nil && got.Status == 401
	})
}

func TestLEN01_1_ARequestJustBeforeTheLengthHasPassedGetsAPrompt(t *testing.T) {
	const length = 5 * time.Second
	w := newWorld(t, "SESSION_LENGTH = 5s")
	time.Sleep(time.Until(w.started.Add(length - margin - 500*time.Millisecond)))
	a := w.ask("echo in time")
	w.prompt(1)
	w.press("y")
	a.answer().ran(t, 0)
}

func TestLEN01_2_TheTimeIsUpAfterTheSessionLength(t *testing.T) {
	const length = 3 * time.Second
	w := newWorld(t, "SESSION_LENGTH = 3s")
	w.noQuestionBefore(w.started.Add(length-margin), 0)
	w.question(1)
	if late := time.Since(w.started) - length; late > 3*time.Second {
		t.Fatalf("the question came %s after the session length had passed", late)
	}
	w.shows(extendQuestion("3 seconds"))
	if !w.sessionTime().TimeUp {
		t.Fatal("the API does not say that the time is up")
	}
}

func TestLEN02_1_AShorterLengthAskedForAtStart(t *testing.T) {
	const length = 3 * time.Second
	// SESSION_LENGTH stays at its starting value, 30 minutes.
	w := newWorldOf(t, "3s")
	w.shows("The session lasts 3 seconds.")
	w.noQuestionBefore(w.started.Add(length-margin), 0)
	w.question(1)
	w.shows(extendQuestion("3 seconds"))
	if !w.sessionTime().TimeUp {
		t.Fatal("the API does not say that the time is up")
	}
}

func TestLEN02_2_ALongerLengthAskedForAtStart(t *testing.T) {
	w := newWorldOf(t, "10m", "SESSION_LENGTH = 2s")
	w.shows("The session lasts 10 minutes.")
	w.noQuestionBefore(w.started.Add(2*time.Second+margin), 0)
	if got := w.sessionTime(); got.TimeUp || got.SecondsLeft < 500 {
		t.Fatalf("the API says %+v, want a session with most of ten minutes left", got)
	}
	w.run("echo still live").ran(t, 0)
	if w.questions() != 0 {
		t.Fatal("the question to extend was shown")
	}
}

func TestLEN03_1_TheQuestionToExtendIsShown(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2s")
	w.question(1)
	w.screenEnds(extendQuestion("2 seconds"))
}

func TestLEN03_2_YMakesTheSessionLiveAgain(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 5s")
	w.question(1)
	// Capitals and Enter do not answer.
	w.press("Y\r")
	w.screenEnds(extendQuestion("5 seconds"))
	w.press("y")
	w.shows(extendQuestion("5 seconds") + "y\nThe session goes on for 5 seconds.\n")
	got := w.run("echo live again")
	got.ran(t, 0)
	if got.Output != "live again\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
	if w.sessionTime().TimeUp {
		t.Fatal("the API still says that the time is up")
	}
}

// extendedOnce presses y at the first question and checks that the time is
// up again after the same length, and not before.
func extendedOnce(t *testing.T, w *world, length time.Duration, words string) {
	t.Helper()
	w.question(1)
	extended := time.Now()
	w.press("y")
	w.shows("The session goes on for " + words + ".")
	w.noQuestionBefore(extended.Add(length-margin/2), 1)
	if w.sessionTime().TimeUp {
		t.Fatal("the API says that the time is up before the second stretch has passed")
	}
	w.question(2)
	if late := time.Since(extended) - length; late > 3*time.Second {
		t.Fatalf("the second question came %s after the same length had passed again", late)
	}
	w.screenEnds(extendQuestion(words))
}

func TestLEN03_3_AnExtendedSessionGetsTheSessionLengthAgain(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 3s")
	extendedOnce(t, w, 3*time.Second, "3 seconds")
}

func TestLEN03_4_AnExtendedSessionGetsItsOwnLengthAgain(t *testing.T) {
	// SESSION_LENGTH stays at 30 minutes: the second stretch is the
	// length asked for at start, not that.
	w := newWorldOf(t, "3s")
	extendedOnce(t, w, 3*time.Second, "3 seconds")
}

func TestLEN04_1_NoAnswerEndsTheSession(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2s", "EXTEND_WAIT = 1s")
	w.live()
	w.question(1)
	w.dead()
	w.shows("No answer in time.\nThe session was not extended: the session ends.\n")
}

func TestLEN04_2_NEndsTheSessionAtOnce(t *testing.T) {
	// EXTEND_WAIT stays at 60 seconds: the end must not wait for it.
	w := newWorld(t, "SESSION_LENGTH = 2s")
	w.live()
	w.question(1)
	pressed := time.Now()
	w.press("n")
	w.dead()
	if took := time.Since(pressed); took > 5*time.Second {
		t.Fatalf("the session ended %s after the n", took)
	}
	w.shows(extendQuestion("2 seconds") + "n\nThe session was not extended: the session ends.\n")
}

func TestLEN06_1_ARequestIsRefusedWhileTheTimeIsUp(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2s")
	w.question(1)
	w.ask("touch "+w.file("ran")).answer().refused(t, "time_up")
	w.promptWaits(0)
	w.notRun("ran")
	w.shows("Refused a request with no prompt: the session's time is up.\n")
	w.screenEnds(extendQuestion("2 seconds"))
}

func TestLEN07_1_AWaitingPromptIsRefusedWhenTheTimeRunsOut(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 3s")
	a := w.ask("touch " + w.file("ran"))
	w.promptShown(1)
	a.answer().refused(t, "time_up")
	w.question(1)
	w.screenEnds("Run it? y/n \nThe session's time is up. Nothing ran.\n" + extendQuestion("3 seconds"))
	w.notRun("ran")
	// A y now answers the question to extend, not the prompt that is gone.
	w.press("y")
	w.shows("The session goes on for 3 seconds.")
	w.promptWaits(1)
	w.notRun("ran")
}

func TestLEN08_1_ARunningCommandIsNotKilledWhenTheTimeRunsOut(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 3s", "COMMAND_TIME_LIMIT = 60s")
	a := w.ask("echo began; sleep 5; echo over")
	w.prompt(1)
	w.press("y")
	w.shows("The session's time is up. The command that runs gets no more time.\n")
	if got := w.sessionTime(); !got.TimeUp || got.Requests.Running == nil {
		t.Fatalf("the API says %+v, want the time up and a command that runs", got)
	}
	a.noAnswerYet()
	if w.questions() != 0 {
		t.Fatal("the question to extend was shown while the command runs")
	}
	// It runs to its own end: it was not killed.
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "began\nover\n" {
		t.Fatalf("want the whole output, got %s", got.Body)
	}
}

func TestLEN08_2_ItIsKilledAtItsNextTimeLimit(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2s", "COMMAND_TIME_LIMIT = 4s")
	a := w.ask("echo began; sleep 30; touch " + w.file("over"))
	w.prompt(1)
	w.press("y")
	approved := time.Now()
	got := a.answer()
	// The AI is not asked to wait or stop: the answer is the end.
	if got.Status != 200 || got.State != "ended" || got.How != "killed_time_up" || got.ExitCode != nil {
		t.Fatalf("want ended with killed_time_up, got %d %s", got.Status, got.Body)
	}
	if took := time.Since(approved); took < 4*time.Second-margin || took > 4*time.Second+5*time.Second {
		t.Fatalf("the command was killed %s after its start, want about its time limit of 4s", took)
	}
	w.shows("Killed: the time limit passed and the session's time was up.\n")
	w.notRun("over")
	w.question(1)
}

func TestLEN08_3_ACommandThatEndsByItselfThenTheQuestion(t *testing.T) {
	w := newWorld(t, "SESSION_LENGTH = 2s", "COMMAND_TIME_LIMIT = 60s")
	a := w.ask("sleep 4; echo over")
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "over\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
	w.question(1)
	w.screenEnds("  | over\n" + extendQuestion("2 seconds"))
}

func TestLEN09_1_NoAPICallMakesTheSessionLonger(t *testing.T) {
	const length = 5 * time.Second
	w := newWorld(t, "SESSION_LENGTH = 5s")
	before := w.sessionTime()
	more := `{"seconds_left":3600,"length_seconds":3600,"extend":true,"time_up":false}`
	for _, c := range []struct{ method, path string }{
		{"POST", "/v1/session/extend"},
		{"POST", "/v1/extend"},
		{"POST", "/v1/session"},
		{"PUT", "/v1/session"},
		{"POST", "/v1/requests/extend/wait"},
		{"POST", "/v1/hello"},
	} {
		got, err := w.call(context.Background(), c.method, c.path, more)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != 404 && got.Status != 405 && got.Status != 401 {
			t.Fatalf("%s %s gave %d %s, want it refused", c.method, c.path, got.Status, got.Body)
		}
	}
	if after := w.sessionTime(); after.SecondsLeft > before.SecondsLeft || after.TimeUp {
		t.Fatalf("the API says %+v after the calls, and said %+v before", after, before)
	}
	// The end time has not moved: the time is up when the length has passed.
	w.noQuestionBefore(w.started.Add(length-margin), 0)
	w.question(1)
	if late := time.Since(w.started) - length; late > 3*time.Second {
		t.Fatalf("the question came %s after the session length had passed", late)
	}
	w.promptWaits(0)
}

func TestLEN09_2_NoMessageFromTheControllerMakesTheSessionLonger(t *testing.T) {
	const length = 4 * time.Second
	beside(t)
	s := standInGiving(t, func(host string) string { return "http://" + host }, false, "SESSION_LENGTH = 4s")
	for _, raw := range []string{
		`{"type":"extend","seconds_left":3600,"length_seconds":3600}`,
		`{"type":"extended","seconds_left":3600}`,
		`{"type":"session_ok","seconds_left":3600,"length_seconds":3600}`,
		`{"type":"session_start","length_seconds":3600}`,
		`{"type":"request","id":"r_1","kind":"extend","seconds_left":3600}`,
		`{"type":"wait","id":"extend"}`,
	} {
		s.send(raw)
	}
	s.noQuestionBefore(s.started.Add(length-margin), 0)
	s.question(1)
	if late := time.Since(s.started) - length; late > 3*time.Second {
		t.Fatalf("the question came %s after the session length had passed", late)
	}
	s.promptWaits(0)
}

func TestLEN09_3_AYesFromTheControllerDoesNotAnswerTheQuestion(t *testing.T) {
	beside(t)
	s := standInGiving(t, func(host string) string { return "http://" + host }, false, "SESSION_LENGTH = 2s", "EXTEND_WAIT = 3s")
	s.question(1)
	for _, raw := range []string{
		`{"type":"extend","answer":"y"}`,
		`{"type":"extended","seconds_left":3600}`,
		`{"type":"y"}`,
		`{"type":"yes"}`,
		`{"type":"answer","answer":"y"}`,
		`{"type":"key","key":"y"}`,
		`{"type":"approved"}`,
		`{"type":"wait","id":"y"}`,
		`{"type":"cancel","id":"y"}`,
	} {
		s.send(raw)
	}
	s.screenEnds(extendQuestion("2 seconds"))
	// A request does not answer it either. The question is shown again
	// under the note, and still waits.
	s.send(`{"type":"request","id":"r_1","kind":"command","command":"y"}`)
	s.shows("Refused a request with no prompt: the session's time is up.\n")
	s.screenEnds(extendQuestion("2 seconds"))
	// Nobody said yes: the session ends when EXTEND_WAIT has passed.
	s.ended()
	if text := s.screen.text(); strings.Contains(text, "The session goes on") || !strings.Contains(text, "No answer in time.") {
		t.Fatalf("the question was answered:\n%s", text)
	}
}

func TestLEN05_1_AtTheMaximumTheSessionEndsWithNoQuestion(t *testing.T) {
	const max = 8 * time.Second
	w := newWorldUnder(t, "SESSION_MAX = 8s", "", "SESSION_LENGTH = 3s")
	w.shows("The session lasts 3 seconds. Then you are asked whether to extend it, up to the maximum of 8 seconds.\n")
	w.question(1)
	w.press("y")
	w.shows("The session goes on for 3 seconds.\n")
	w.question(2)
	// Less than a full stretch is left: the question names what is left.
	if !regexp.MustCompile(`\nThe session's time is up\.\nExtend it by [12] seconds?, up to the maximum\? y/n $`).MatchString(w.screen.text()) {
		t.Fatalf("the second question does not name what is left to the maximum")
	}
	w.press("y")
	w.shows("That reaches the maximum: then it ends.\n")
	w.live()

	w.dead()
	if lasted := time.Since(w.started); lasted < max-margin || lasted > max+3*time.Second {
		t.Fatalf("the session ended after %s, want at the maximum of %s", lasted.Round(time.Millisecond), max)
	}
	if got := w.questions(); got != 2 {
		t.Fatalf("%d questions to extend were shown, want 2: none at the maximum", got)
	}
	w.shows("The session has lasted 8 seconds, the maximum: the session ends.\n")
}

func TestLEN05_2_AMaximumShorterThanTheSessionLength(t *testing.T) {
	const max = 3 * time.Second
	// SESSION_LENGTH stays at its starting value, 30 minutes.
	w := newWorldUnder(t, "SESSION_MAX = 3s", "")
	w.live()
	w.dead()
	if lasted := time.Since(w.started); lasted < max-margin || lasted > max+3*time.Second {
		t.Fatalf("the session ended after %s, want at the maximum of %s", lasted.Round(time.Millisecond), max)
	}
	if got := w.questions(); got != 0 {
		t.Fatalf("%d questions to extend were shown, want none", got)
	}
}
