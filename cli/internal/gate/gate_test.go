package gate

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kdop/gangway/cli/internal/request"
)

const minShow = 500 * time.Millisecond

type refusal struct{ id, reason string }

type answered struct {
	req    request.Request
	answer Answer
}

// world is every neighbour of the gate, faked. It records what it was asked.
type world struct {
	g *Gate

	queue []request.Request
	now   time.Time

	prompts []request.Request
	shownAt time.Time
	answers []answered
	started []string
	refused []refusal
	runs    []*Approval
}

func (w *world) Next() (request.Request, bool) {
	if len(w.queue) == 0 {
		return request.Request{}, false
	}
	req := w.queue[0]
	w.queue = w.queue[1:]
	return req, true
}
func (w *world) Prompt(r request.Request) {
	w.prompts = append(w.prompts, r)
	w.shownAt = w.now
}
func (w *world) Answer(r request.Request, a Answer) { w.answers = append(w.answers, answered{r, a}) }
func (w *world) Started(id string)                  { w.started = append(w.started, id) }
func (w *world) Refused(id, reason string)          { w.refused = append(w.refused, refusal{id, reason}) }
func (w *world) Run(a *Approval)                    { w.runs = append(w.runs, a) }
func (w *world) Now() time.Time                     { return w.now }

func (w *world) pass(d time.Duration) { w.now = w.now.Add(d) }

// sent counts every message that went to the controller.
func (w *world) sent() int { return len(w.started) + len(w.refused) }

var (
	reqA = request.Request{ID: "a", Command: "echo a | tail -n 1"}
	reqB = request.Request{ID: "b", Command: "echo b"}
)

func key(s string) []byte { return []byte(s) }

// free gives a gate in Free, with these requests in the line.
func free(reqs ...request.Request) *world {
	w := &world{now: time.Unix(1000, 0), queue: reqs}
	w.g = New(w, w, w, w, w, minShow)
	return w
}

// asking gives a gate in Asking for reqA, with PROMPT_MIN_SHOW passed and
// these requests behind it.
func asking(behind ...request.Request) *world {
	w := free(append([]request.Request{reqA}, behind...)...)
	w.g.Ready()
	w.pass(minShow)
	return w
}

// busy gives a gate in Busy with reqA, and these requests in the line.
func busy(behind ...request.Request) *world {
	w := asking(behind...)
	w.g.Key(key("y"))
	return w
}

func ended() *world {
	w := free()
	w.g.End()
	return w
}

func wantState(t *testing.T, w *world, want State) {
	t.Helper()
	if got := w.g.State(); got != want {
		t.Fatalf("state is %v, want %v", got, want)
	}
}

func wantRuns(t *testing.T, w *world, want int) {
	t.Helper()
	if got := len(w.runs); got != want {
		t.Fatalf("the runner was called %d times, want %d", got, want)
	}
}

func wantRefused(t *testing.T, w *world, want ...refusal) {
	t.Helper()
	if fmt.Sprint(w.refused) != fmt.Sprint(want) {
		t.Fatalf("refused is %v, want %v", w.refused, want)
	}
}

// UNIT-GATE-01. Requirement: GATE-01, GATE-05.
func TestUNITGATE01_ReadyShowsThePrompt(t *testing.T) {
	w := free(reqA)
	w.g.Ready()
	wantState(t, w, Asking)
	if len(w.prompts) != 1 || w.prompts[0] != reqA {
		t.Fatalf("prompts are %v, want exactly %v", w.prompts, reqA)
	}
	wantRuns(t, w, 0)
}

// UNIT-GATE-02. Requirement: GATE-01, GATE-05.
func TestUNITGATE02_YApproves(t *testing.T) {
	w := asking()
	w.g.Key(key("y"))
	wantState(t, w, Busy)
	if fmt.Sprint(w.started) != "[a]" {
		t.Fatalf("started is %v, want [a]", w.started)
	}
	wantRuns(t, w, 1)
	got, ok := w.runs[0].Use()
	if !ok || got != w.prompts[0] {
		t.Fatalf("the approval holds %v (ok=%v), want the prompted %v", got, ok, w.prompts[0])
	}
	wantRefused(t, w)
}

// UNIT-GATE-03. Requirement: GATE-02.
func TestUNITGATE03_NRefuses(t *testing.T) {
	w := asking()
	w.g.Key(key("n"))
	wantState(t, w, Free)
	wantRefused(t, w, refusal{"a", ReasonUser})
	wantRuns(t, w, 0)
}

// UNIT-GATE-04. Requirement: GATE-03.
func TestUNITGATE04_OtherKeysAreIgnored(t *testing.T) {
	w := asking()
	for _, k := range []string{"Y", "N", "\r", "\n", " ", "\x1b", "\x1b[A", "x", "yes", "ye", "ny", ""} {
		w.g.Key(key(k))
		wantState(t, w, Asking)
		if w.sent() != 0 || len(w.answers) != 0 || len(w.runs) != 0 {
			t.Fatalf("key %q did something: sent %d, answers %d, runs %d", k, w.sent(), len(w.answers), len(w.runs))
		}
	}
}

// UNIT-GATE-05. Requirement: LIM-09.
func TestUNITGATE05_CtrlCRefuses(t *testing.T) {
	w := asking()
	w.g.CtrlC()
	wantState(t, w, Free)
	wantRefused(t, w, refusal{"a", ReasonUser})
	wantRuns(t, w, 0)
}

// UNIT-GATE-06. Requirement: GATE-12.
func TestUNITGATE06_CancelOfTheFrontRefuses(t *testing.T) {
	w := asking()
	w.g.Cancel("a")
	wantState(t, w, Free)
	wantRefused(t, w, refusal{"a", ReasonAIGone})
	wantRuns(t, w, 0)
}

// UNIT-GATE-07. Requirement: GATE-12.
func TestUNITGATE07_CancelOfAnotherIsNotTheGates(t *testing.T) {
	w := asking()
	w.g.Cancel("b")
	wantState(t, w, Asking)
	if w.sent() != 0 {
		t.Fatalf("something was sent: %v %v", w.started, w.refused)
	}
	w.g.Key(key("y"))
	if got, _ := w.runs[0].Use(); got != reqA {
		t.Fatalf("the front is %v, want %v", got, reqA)
	}
}

// UNIT-GATE-08. Requirement: GATE-01.
func TestUNITGATE08_EndWhileAsking(t *testing.T) {
	w := asking()
	w.g.End()
	wantState(t, w, Ended)
	wantRefused(t, w, refusal{"a", ReasonSessionEnded})
	wantRuns(t, w, 0)
}

// UNIT-GATE-09. Requirement: GATE-04.
func TestUNITGATE09_EarlyKeyInFreeIsNotKept(t *testing.T) {
	w := free()
	w.g.Key(key("y"))
	w.queue = append(w.queue, reqA)
	w.g.Ready()
	w.pass(minShow)
	wantState(t, w, Asking)
	wantRuns(t, w, 0)
}

// UNIT-GATE-10. Requirement: GATE-04.
func TestUNITGATE10_KeyInBusyIsNotKept(t *testing.T) {
	w := busy(reqB)
	w.g.Key(key("y"))
	w.g.Done()
	w.pass(minShow)
	wantState(t, w, Asking)
	if last := w.prompts[len(w.prompts)-1]; last != reqB {
		t.Fatalf("the prompt is for %v, want %v", last, reqB)
	}
	wantRuns(t, w, 1)
}

// UNIT-GATE-11. Requirement: GATE-06.
func TestUNITGATE11_ReadyInBusyShowsNoPrompt(t *testing.T) {
	w := busy(reqB)
	w.g.Ready()
	wantState(t, w, Busy)
	if len(w.prompts) != 1 {
		t.Fatalf("%d prompts were shown, want 1", len(w.prompts))
	}
	if len(w.queue) != 1 || w.queue[0] != reqB {
		t.Fatalf("the line is %v, want it to still hold %v", w.queue, reqB)
	}
}

// UNIT-GATE-12. Requirement: GATE-02, GATE-06, GATE-14.
func TestUNITGATE12_NTwiceRefusesOnlyTheFirst(t *testing.T) {
	w := asking(reqB)
	w.g.Key(key("n"))
	w.g.Key(key("n"))
	wantState(t, w, Asking)
	wantRefused(t, w, refusal{"a", ReasonUser})
	if last := w.prompts[len(w.prompts)-1]; last != reqB {
		t.Fatalf("the prompt is for %v, want %v", last, reqB)
	}
}

// UNIT-GATE-13. Requirement: GATE-06.
func TestUNITGATE13_DoneWithAnEmptyLine(t *testing.T) {
	w := busy()
	w.g.Done()
	wantState(t, w, Free)
	if len(w.prompts) != 1 {
		t.Fatalf("%d prompts were shown, want 1", len(w.prompts))
	}
}

// events is every event of the table.
var events = []struct {
	name string
	do   func(w *world)
}{
	{"ready", func(w *world) { w.g.Ready() }},
	{"key y", func(w *world) { w.g.Key(key("y")) }},
	{"key n", func(w *world) { w.g.Key(key("n")) }},
	{"key x", func(w *world) { w.g.Key(key("x")) }},
	{"ctrl_c", func(w *world) { w.g.CtrlC() }},
	{"cancel front", func(w *world) { w.g.Cancel("a") }},
	{"cancel other", func(w *world) { w.g.Cancel("zz") }},
	{"done", func(w *world) { w.g.Done() }},
	{"end", func(w *world) { w.g.End() }},
}

// UNIT-GATE-14. Requirement: SES-03.
func TestUNITGATE14_EndedIsFinal(t *testing.T) {
	for _, e := range events {
		w := ended()
		w.queue = append(w.queue, reqA)
		e.do(w)
		wantState(t, w, Ended)
		if len(w.prompts)+len(w.answers)+w.sent()+len(w.runs) != 0 {
			t.Fatalf("%s did something in Ended", e.name)
		}
	}
}

// UNIT-GATE-15. Requirement: GATE-01, GATE-07.
func TestUNITGATE15_OnlyYInAskingReachesTheRunner(t *testing.T) {
	states := []struct {
		name  string
		make  func() *world
		calls int // runner calls made to reach the state
	}{
		{"Free", func() *world { return free() }, 0},
		{"Free, line not empty", func() *world { w := free(); w.queue = append(w.queue, reqB); return w }, 0},
		{"Asking", func() *world { return asking(reqB) }, 0},
		{"Busy", func() *world { return busy(reqB) }, 1},
		{"Ended", ended, 0},
	}
	for _, s := range states {
		for _, e := range events {
			w := s.make()
			if w.g.State() == Asking && e.name == "key y" {
				continue
			}
			e.do(w)
			if len(w.runs) != s.calls {
				t.Fatalf("%s in %s called the runner", e.name, s.name)
			}
		}
	}
}

// UNIT-GATE-16. Requirement: GATE-01, GATE-05, GATE-07.
func TestUNITGATE16_RandomWalk(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	w := free()
	wantYes := 0
	next := 0
	for step := 0; step < 20000; step++ {
		front := request.Request{}
		if len(w.prompts) > 0 {
			front = w.prompts[len(w.prompts)-1]
		}
		switch rng.Intn(10) {
		case 0, 1:
			if len(w.queue) < 5 {
				next++
				id := fmt.Sprintf("r%d", next)
				w.queue = append(w.queue, request.Request{ID: id, Command: "cmd " + id})
			}
			w.g.Ready()
		case 2, 3:
			counts := w.g.State() == Asking && w.now.Sub(w.shownAt) >= minShow
			before := len(w.runs)
			w.g.Key(key("y"))
			if ran := len(w.runs) > before; ran != counts {
				t.Fatalf("step %d: y ran=%v, want %v", step, ran, counts)
			}
			if len(w.runs) > before {
				wantYes++
				if got, ok := w.runs[before].Use(); !ok || got != front {
					t.Fatalf("step %d: ran %v, want the front %v", step, got, front)
				}
			}
		case 4:
			w.g.Key(key("n"))
		case 5:
			w.g.Key(key("x"))
		case 6:
			w.g.CtrlC()
		case 7:
			if rng.Intn(2) == 0 {
				w.g.Cancel(front.ID)
			} else {
				w.g.Cancel("zz")
			}
		case 8:
			w.g.Done()
		case 9:
			w.pass(time.Duration(rng.Int63n(int64(2 * minShow))))
		}
		if len(w.runs) != wantYes {
			t.Fatalf("step %d: %d runner calls, want %d", step, len(w.runs), wantYes)
		}
	}
	w.g.End()

	if wantYes == 0 {
		t.Fatal("the walk never approved anything, so it proves nothing")
	}
	answers := map[string]int{}
	for _, id := range w.started {
		answers[id]++
	}
	for _, r := range w.refused {
		answers[r.id]++
	}
	for _, p := range w.prompts {
		if answers[p.ID] != 1 {
			t.Fatalf("request %s got %d answers, want 1", p.ID, answers[p.ID])
		}
	}
	if len(answers) != len(w.prompts) {
		t.Fatalf("%d requests were answered, %d were taken", len(answers), len(w.prompts))
	}
}

// UNIT-GATE-17. Requirement: GATE-15.
func TestUNITGATE17_AnApprovalWorksOnce(t *testing.T) {
	w := asking()
	w.g.Key(key("y"))
	a := w.runs[0]
	if _, ok := a.Use(); !ok {
		t.Fatal("a fresh approval did not work")
	}
	if _, ok := a.Use(); ok {
		t.Fatal("a used approval worked a second time")
	}
	if _, ok := (&Approval{}).Use(); ok {
		t.Fatal("an approval made with no gate worked")
	}
	var none *Approval
	if _, ok := none.Use(); ok {
		t.Fatal("a nil approval worked")
	}
}

// UNIT-GATE-18. Requirement: GATE-14.
func TestUNITGATE18_AFreshPromptTakesNoKey(t *testing.T) {
	w := free(reqA)
	w.g.Ready()
	w.g.Key(key("y"))
	w.g.Key(key("n"))
	w.pass(minShow - time.Nanosecond)
	w.g.Key(key("y"))
	wantState(t, w, Asking)
	if w.sent() != 0 || len(w.runs) != 0 {
		t.Fatalf("an early key did something: sent %d, runs %d", w.sent(), len(w.runs))
	}
}

// UNIT-GATE-19. Requirement: GATE-14.
func TestUNITGATE19_AfterTheWaitYApproves(t *testing.T) {
	w := free(reqA)
	w.g.Ready()
	w.g.Key(key("y"))
	w.pass(minShow)
	w.g.Key(key("y"))
	wantState(t, w, Busy)
	wantRuns(t, w, 1)
}

// UNIT-GATE-20. Requirement: GATE-14, LIM-09.
func TestUNITGATE20_CtrlCIsNotHeldBack(t *testing.T) {
	w := free(reqA)
	w.g.Ready()
	w.g.CtrlC()
	wantState(t, w, Free)
	wantRefused(t, w, refusal{"a", ReasonUser})
}

// UNIT-GATE-21. Requirement: GATE-12, GATE-14.
func TestUNITGATE21_YAfterACancelDoesNotAnswerTheNext(t *testing.T) {
	w := asking(reqB)
	w.g.Cancel("a")
	w.g.Key(key("y"))
	wantState(t, w, Asking)
	if last := w.prompts[len(w.prompts)-1]; last != reqB {
		t.Fatalf("the prompt is for %v, want %v", last, reqB)
	}
	wantRuns(t, w, 0)
}

// Every UNIT-GATE test of design.md must have a Go test in this file, with
// the same requirements in the comment above it. Every requirement named
// must be in requirements.md.
func TestDesignAndGoTestsMatch(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	design := read("../../../design.md")
	requirements := read("../../../requirements.md")
	code := read("gate_test.go")

	rows := regexp.MustCompile(`(?m)^\| UNIT-GATE-(\d+) \| ([^|]*) \|`).FindAllStringSubmatch(design, -1)
	if len(rows) == 0 {
		t.Fatal("found no UNIT-GATE test in design.md")
	}
	for _, row := range rows {
		num, reqs := row[1], strings.ReplaceAll(row[2], "`", "")
		if reqs == "" {
			t.Errorf("UNIT-GATE-%s names no requirement in design.md", num)
		}
		for _, id := range strings.Split(reqs, ", ") {
			if !strings.Contains(requirements, "\n### "+id+"\n") {
				t.Errorf("UNIT-GATE-%s names %s, which is not in requirements.md", num, id)
			}
		}
		want := "// UNIT-GATE-" + num + ". Requirement: " + reqs + ".\nfunc TestUNITGATE" + num + "_"
		if !strings.Contains(code, want) {
			t.Errorf("UNIT-GATE-%s: no Go test with the comment %q", num, "// UNIT-GATE-"+num+". Requirement: "+reqs+".")
		}
	}
	if n := len(regexp.MustCompile(`(?m)^func TestUNITGATE\d+_`).FindAllString(code, -1)); n != len(rows) {
		t.Errorf("%d Go tests carry a UNIT-GATE number, design.md has %d", n, len(rows))
	}
}
