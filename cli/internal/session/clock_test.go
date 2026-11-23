package session

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	extendWait = 60 * time.Second
	minShow    = 500 * time.Millisecond
)

type started struct {
	d      time.Duration
	number int
}

// world is every neighbour of the session's clock, faked. It records what
// it was asked.
type world struct {
	s *Session

	now    time.Time
	timers []started

	questions []time.Duration // the question to extend, with the length it named
	answers   []Answer
	sent      []string // what went to the controller
	refuse    int      // the loop was told to refuse what waits
	notExt    int      // the loop was told to end the session
}

func (w *world) Now() time.Time                    { return w.now }
func (w *world) Start(d time.Duration, number int) { w.timers = append(w.timers, started{d, number}) }
func (w *world) ExtendQuestion(d time.Duration)    { w.questions = append(w.questions, d) }
func (w *world) ExtendAnswer(a Answer)             { w.answers = append(w.answers, a) }
func (w *world) TimeUp()                           { w.sent = append(w.sent, "time_up") }
func (w *world) Extended(seconds int)              { w.sent = append(w.sent, fmt.Sprint("extended ", seconds)) }

// loopSide is the loop, as the session sees it.
type loopSide struct{ w *world }

func (l loopSide) TimeUp() { l.w.refuse++ }
func (l loopSide) NotExtended() {
	l.w.notExt++
	l.w.s.End()
}

func (w *world) pass(d time.Duration) { w.now = w.now.Add(d) }

// last gives the timer that was started last.
func (w *world) last() started { return w.timers[len(w.timers)-1] }

// did tells everything the session has done so far, to compare later.
func (w *world) did() string {
	return fmt.Sprint(w.s.State(), w.s.end, w.timers, w.questions, w.answers, w.sent, w.refuse, w.notExt)
}

// clocked gives a live session whose clock has just started with this
// length.
func clocked(t *testing.T, length time.Duration) *world {
	t.Helper()
	w := &world{now: time.Unix(1000, 0), s: mustNew(t)}
	w.s.StartClock(Edges{Clock: w, Timers: w, Screen: w, Sender: w, Loop: loopSide{w},
		Length: length, ExtendWait: extendWait, MinShow: minShow})
	return w
}

// timeUp gives a session whose time has just run out.
func timeUp(t *testing.T, length time.Duration) *world {
	t.Helper()
	w := clocked(t, length)
	w.pass(length)
	w.s.Timer(w.last().number)
	return w
}

// extending gives a session with the question on screen and
// PROMPT_MIN_SHOW passed.
func extending(t *testing.T, length time.Duration) *world {
	t.Helper()
	w := timeUp(t, length)
	w.s.Idle()
	w.pass(minShow)
	return w
}

// UNIT-SESSION-10. Requirement: LEN-01, LEN-02.
func TestUNITSESSION10_TheClockCountsTheLength(t *testing.T) {
	for _, length := range []time.Duration{30 * time.Minute, 10 * time.Minute} {
		w := clocked(t, length)
		if len(w.timers) != 1 || w.last().d != length {
			t.Fatalf("with a length of %s the timers are %v", length, w.timers)
		}
		w.pass(length - time.Millisecond)
		if !w.s.Takes() || w.s.State() != Live {
			t.Fatalf("a request just before %s has passed may not go on", length)
		}
		if len(w.sent)+len(w.questions)+w.refuse != 0 {
			t.Fatalf("something was sent or shown: %s", w.did())
		}
	}
}

// UNIT-SESSION-11. Requirement: LEN-01, LEN-06.
func TestUNITSESSION11_TheTimerMakesTheTimeUp(t *testing.T) {
	w := timeUp(t, 30*time.Minute)
	if w.s.State() != TimeUp {
		t.Fatalf("the state is %s, want TimeUp", w.s.State())
	}
	if fmt.Sprint(w.sent) != "[time_up]" || w.refuse != 1 {
		t.Fatalf("sent %v, and the loop was told %d times to refuse", w.sent, w.refuse)
	}
	if w.s.Takes() {
		t.Fatal("a request may go on after the time is up")
	}
	if len(w.questions) != 0 {
		t.Fatal("the question was shown before the loop said that no command runs")
	}
	if fmt.Sprint(w.sent) != "[time_up]" || w.refuse != 1 {
		t.Fatalf("the request made it send again: %v, %d", w.sent, w.refuse)
	}
}

// UNIT-SESSION-12. Requirement: LEN-03, LEN-08.
func TestUNITSESSION12_TheQuestionComesWhenNoCommandRuns(t *testing.T) {
	w := timeUp(t, 10*time.Minute)
	before := len(w.timers)
	w.s.Idle()
	w.s.Idle()
	if w.s.State() != Extending {
		t.Fatalf("the state is %s, want Extending", w.s.State())
	}
	if fmt.Sprint(w.questions) != fmt.Sprint([]time.Duration{10 * time.Minute}) {
		t.Fatalf("the questions shown: %v, want one for ten minutes", w.questions)
	}
	if len(w.timers) != before+1 || w.last().d != extendWait {
		t.Fatalf("the timers are %v, want one more, of EXTEND_WAIT", w.timers)
	}
}

// UNIT-SESSION-13. Requirement: LEN-03.
func TestUNITSESSION13_YExtends(t *testing.T) {
	w := extending(t, 10*time.Minute)
	before := len(w.timers)
	w.s.Press([]byte("y"))
	if w.s.State() != Live {
		t.Fatalf("the state is %s, want Live", w.s.State())
	}
	if fmt.Sprint(w.answers) != fmt.Sprint([]Answer{Yes}) {
		t.Fatalf("the answers shown: %v", w.answers)
	}
	if fmt.Sprint(w.sent) != "[time_up extended 600]" {
		t.Fatalf("sent %v", w.sent)
	}
	if len(w.timers) != before+1 || w.last().d != 10*time.Minute {
		t.Fatalf("the timers are %v, want one more, of ten minutes", w.timers)
	}
	if !w.s.Takes() {
		t.Fatal("a request may not go on after the session was extended")
	}
	if w.notExt != 0 {
		t.Fatal("the loop was told to end the session")
	}
}

// UNIT-SESSION-14. Requirement: LEN-03.
func TestUNITSESSION14_TheSameLengthAgain(t *testing.T) {
	w := extending(t, 10*time.Minute)
	old := append([]started(nil), w.timers...)
	w.s.Press([]byte("y"))
	w.pass(10*time.Minute - time.Millisecond)
	if !w.s.Takes() {
		t.Fatal("the second stretch is shorter than the first")
	}
	for _, timer := range old {
		w.s.Timer(timer.number)
	}
	if w.s.State() != Live || fmt.Sprint(w.sent) != "[time_up extended 600]" {
		t.Fatalf("an old timer did something: %s", w.did())
	}
	w.pass(time.Millisecond)
	w.s.Timer(w.last().number)
	if w.s.State() != TimeUp || fmt.Sprint(w.sent) != "[time_up extended 600 time_up]" || w.refuse != 2 {
		t.Fatalf("after the second stretch: %s", w.did())
	}
}

// UNIT-SESSION-15. Requirement: LEN-04.
func TestUNITSESSION15_NoAndNoAnswerEndTheSession(t *testing.T) {
	for name, c := range map[string]struct {
		do   func(w *world)
		want Answer
	}{
		"n":         {func(w *world) { w.pass(minShow); w.s.Press([]byte("n")) }, No},
		"ctrl_c":    {func(w *world) { w.s.CtrlC() }, No},
		"no answer": {func(w *world) { w.pass(extendWait); w.s.Timer(w.last().number) }, NoAnswer},
	} {
		w := timeUp(t, 10*time.Minute)
		w.s.Idle()
		timers := len(w.timers)
		c.do(w)
		if w.notExt != 1 || w.s.State() != Ended {
			t.Fatalf("%s: the loop was told %d times to end, and the state is %s", name, w.notExt, w.s.State())
		}
		if fmt.Sprint(w.answers) != fmt.Sprint([]Answer{c.want}) {
			t.Fatalf("%s: the answers shown: %v", name, w.answers)
		}
		if len(w.timers) != timers || fmt.Sprint(w.sent) != "[time_up]" {
			t.Fatalf("%s: something was extended: %s", name, w.did())
		}
	}
}

// UNIT-SESSION-16. Requirement: LEN-03.
func TestUNITSESSION16_OnlyYAndNAnswerAndNotAtOnce(t *testing.T) {
	w := timeUp(t, 10*time.Minute)
	w.s.Idle()
	before := w.did()
	w.s.Press([]byte("y"))
	w.s.Press([]byte("n"))
	if w.did() != before {
		t.Fatalf("a key answered a question that was just shown: %s", w.did())
	}
	w.pass(minShow)
	for _, key := range []string{"Y", "N", "\r", " ", "\x1b", "\x1b[A", "yy", ""} {
		w.s.Press([]byte(key))
		if w.did() != before {
			t.Fatalf("the key %q did something: %s", key, w.did())
		}
	}
}

// UNIT-SESSION-17. Requirement: LEN-09.
func TestUNITSESSION17_NothingButYAtTheQuestionMovesTheEnd(t *testing.T) {
	w := clocked(t, 10*time.Minute)
	before := w.did()
	w.pass(time.Minute)
	w.s.Press([]byte("y"))
	w.s.CtrlC()
	w.s.Idle()
	if w.did() != before {
		t.Fatalf("in Live something moved: %s, before %s", w.did(), before)
	}

	w = timeUp(t, 10*time.Minute)
	before = w.did()
	w.pass(time.Minute)
	w.s.Press([]byte("y"))
	w.s.CtrlC()
	if w.did() != before {
		t.Fatalf("in TimeUp something moved: %s, before %s", w.did(), before)
	}
}

// UNIT-SESSION-18. Requirement: LEN-06.
func TestUNITSESSION18_ALateTimerLetsNothingIn(t *testing.T) {
	w := clocked(t, 10*time.Minute)
	late := w.last().number
	w.pass(10 * time.Minute)
	if w.s.Takes() {
		t.Fatal("a request may go on at the end time, because the timer has not come")
	}
	if w.s.State() != TimeUp || fmt.Sprint(w.sent) != "[time_up]" || w.refuse != 1 {
		t.Fatalf("the request did not make the time up: %s", w.did())
	}
	before := w.did()
	w.s.Timer(late)
	if w.did() != before {
		t.Fatalf("the late timer did something more: %s", w.did())
	}
}

// UNIT-SESSION-19. Requirement: SES-03, LEN-04.
func TestUNITSESSION19_AfterTheEndTheClockDoesNothing(t *testing.T) {
	for name, make := range map[string]func() *world{
		"Live":      func() *world { return clocked(t, 10*time.Minute) },
		"TimeUp":    func() *world { return timeUp(t, 10*time.Minute) },
		"Extending": func() *world { return extending(t, 10*time.Minute) },
	} {
		w := make()
		timers := append([]started(nil), w.timers...)
		w.s.End()
		before := w.did()
		w.pass(time.Hour)
		for _, timer := range timers {
			w.s.Timer(timer.number)
		}
		w.s.Timer(w.last().number + 1)
		w.s.Idle()
		w.s.Press([]byte("y"))
		w.s.Press([]byte("n"))
		w.s.CtrlC()
		w.s.StartClock(Edges{Clock: w, Timers: w, Screen: w, Sender: w, Loop: loopSide{w}, Length: time.Hour})
		if w.s.Takes() {
			t.Fatalf("from %s: a request may go on after the end", name)
		}
		if w.s.State() != Ended || w.did() != before {
			t.Fatalf("from %s: something happened after the end: %s, before %s", name, w.did(), before)
		}
	}
}

// UNIT-SESSION-20. Requirement: LEN-09.
func TestUNITSESSION20_OnePlaceSetsTheEndTime(t *testing.T) {
	code, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(code)
	if n := strings.Count(src, "s.end = ") + strings.Count(src, "s.end="); n != 1 {
		t.Fatalf("the end time is set in %d places, want one", n)
	}
	if strings.Contains(src, "&s.end") {
		t.Fatal("the end time is changed in a way this test does not follow")
	}
	set := src[strings.Index(src, "func (s *Session) count()"):]
	set = set[:strings.Index(set, "\n}\n")]
	if !strings.Contains(set, "s.end = ") {
		t.Fatal("the end time is not set in count")
	}
	// count is called from the start of the clock and from y at the
	// question, and from nowhere else.
	if n := strings.Count(src, "s.count()"); n != 2 {
		t.Fatalf("count is called from %d places, want two", n)
	}
	for _, from := range []string{"func (s *Session) StartClock(", "func (s *Session) Press("} {
		body := src[strings.Index(src, from):]
		body = body[:strings.Index(body, "\n}\n")]
		if !strings.Contains(body, "s.count()") {
			t.Fatalf("%s does not call count", from)
		}
	}
	// In Press the call sits under the key y.
	press := src[strings.Index(src, "func (s *Session) Press("):]
	press = press[:strings.Index(press, "\n}\n")]
	if y, n := strings.Index(press, "case 'y':"), strings.Index(press, "case 'n':"); y < 0 || n < y ||
		!strings.Contains(press[y:n], "s.count()") {
		t.Fatal("count is not called under the key y")
	}
}
