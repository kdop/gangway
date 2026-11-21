package relay

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

// host gathers what the relay sends to the host.
type host struct {
	mu   sync.Mutex
	sent []messages.Message
}

func (h *host) send(m messages.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, m)
}

// types gives the types of the messages sent, in order.
func (h *host) types() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []string{}
	for _, m := range h.sent {
		out = append(out, m.Type)
	}
	return fmt.Sprint(out)
}

func newRelay() (*Relay, *host) {
	h := &host{}
	return New(h.send), h
}

// opened gives a relay with one request in Open.
func opened() (*Relay, *host, string, Call) {
	r, h := newRelay()
	id, call := r.Ask("echo hi")
	return r, h, id, call
}

// askedState gives a relay with one request in Asked. The call that got the
// running answer comes back too.
func askedState() (*Relay, *host, string, Call) {
	r, h, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: id})
	<-call
	return r, h, id, call
}

func code(n int) *int { return &n }

// got gives the answer of a call, or fails when there is none yet.
func got(t *testing.T, call Call) Answer {
	t.Helper()
	select {
	case a := <-call:
		return a
	default:
		t.Fatal("the call has no answer")
		return Answer{}
	}
}

func wantNoAnswer(t *testing.T, call Call) {
	t.Helper()
	select {
	case a := <-call:
		t.Fatalf("the call got an answer: %+v", a)
	default:
	}
}

func (r *Relay) held() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

func (r *Relay) stateOf(id string) (state, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.reqs[id]
	if rec == nil {
		return 0, false
	}
	return rec.state, true
}

// UNIT-RELAY-01. Requirement: GATE-05.
func TestUNITRELAY01_AskSendsTheExactCommand(t *testing.T) {
	r, h := newRelay()
	command := "grep ERROR app.log |\n\ttail -n 20"
	id, _ := r.Ask(command)
	if len(h.sent) != 1 {
		t.Fatalf("%d messages were sent, want 1", len(h.sent))
	}
	want := messages.Message{Type: messages.TypeRequest, ID: id, Kind: messages.KindCommand, Command: command}
	if h.sent[0] != want {
		t.Fatalf("sent %+v, want %+v", h.sent[0], want)
	}
	if len(id) != 18 || id[:2] != "r_" {
		t.Fatalf("the id is %q, want r_ and 16 hex characters", id)
	}
}

// UNIT-RELAY-02. Requirement: GATE-02.
func TestUNITRELAY02_RefusedPassesTheReason(t *testing.T) {
	r, _, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeRefused, ID: id, Reason: messages.ReasonUser})
	if a := got(t, call); a.State != StateRefused || a.Reason != messages.ReasonUser {
		t.Fatalf("the answer is %+v", a)
	}
	if r.held() != 0 {
		t.Fatal("the request is still held")
	}
}

// UNIT-RELAY-03. Requirement: GATE-01.
func TestUNITRELAY03_EndedCarriesTheOutputInOrder(t *testing.T) {
	r, _, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "one\n"})
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "two\n"})
	wantNoAnswer(t, call)
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0)})
	a := got(t, call)
	if a.State != StateEnded || a.How != messages.HowExit || a.ExitCode == nil || *a.ExitCode != 0 || a.Output != "one\ntwo\n" {
		t.Fatalf("the answer is %+v", a)
	}
	if a.Cut || a.BytesReplaced {
		t.Fatalf("cut or bytes_replaced is set: %+v", a)
	}
}

// UNIT-RELAY-04. Requirement: SHOW-03.
func TestUNITRELAY04_CutAndBytesReplacedPass(t *testing.T) {
	r, _, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0), Cut: true, BytesReplaced: true})
	if a := got(t, call); !a.Cut || !a.BytesReplaced {
		t.Fatalf("the answer is %+v", a)
	}
}

// UNIT-RELAY-05. Requirement: LIM-01.
func TestUNITRELAY05_StillRunningAnswersTheCall(t *testing.T) {
	r, _, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "so far\n"})
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: id})
	if a := got(t, call); a.State != StateRunning || a.ID != id || a.Output != "so far\n" {
		t.Fatalf("the answer is %+v", a)
	}
	if s, ok := r.stateOf(id); !ok || s != asked {
		t.Fatal("the request is not Asked")
	}
}

// UNIT-RELAY-06. Requirement: LIM-02.
func TestUNITRELAY06_WaitGivesOnlyTheNewOutput(t *testing.T) {
	r, h, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "old\n"})
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: id})
	<-call
	next, ok := r.Wait(id)
	if !ok {
		t.Fatal("wait found no such command")
	}
	if last := h.sent[len(h.sent)-1]; last != (messages.Message{Type: messages.TypeWait, ID: id}) {
		t.Fatalf("sent %+v, want wait", last)
	}
	wantNoAnswer(t, next)
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "new\n"})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0)})
	if a := got(t, next); a.State != StateEnded || a.Output != "new\n" {
		t.Fatalf("the answer is %+v", a)
	}
}

// UNIT-RELAY-07. Requirement: LIM-02.
func TestUNITRELAY07_StopIsSentAndAnswered(t *testing.T) {
	r, h, id, _ := askedState()
	next, ok := r.Stop(id)
	if !ok {
		t.Fatal("stop found no such command")
	}
	if last := h.sent[len(h.sent)-1]; last != (messages.Message{Type: messages.TypeStop, ID: id}) {
		t.Fatalf("sent %+v, want stop", last)
	}
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledByAI})
	if a := got(t, next); a.State != StateEnded || a.How != messages.HowKilledByAI || a.ExitCode != nil {
		t.Fatalf("the answer is %+v", a)
	}
}

// UNIT-RELAY-08. Requirement: LIM-04.
func TestUNITRELAY08_AResultIsKeptForTheNextCall(t *testing.T) {
	r, h, id, _ := askedState()
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "late\n"})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(2)})
	// The result is settled. The host cannot change it afterwards.
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "more\n"})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledByUser})
	r.FromHost(messages.Message{Type: messages.TypeRefused, ID: id, Reason: messages.ReasonUser})
	before := len(h.sent)
	next, ok := r.Wait(id)
	if !ok {
		t.Fatal("wait found no such command")
	}
	if a := got(t, next); a.State != StateEnded || a.How != messages.HowExit || *a.ExitCode != 2 || a.Output != "late\n" {
		t.Fatalf("the answer is %+v", a)
	}
	if len(h.sent) != before {
		t.Fatalf("a message was sent to the host: %s", h.types())
	}
	if _, ok := r.Wait(id); ok {
		t.Fatal("a second wait found the command again")
	}
	if _, ok := r.Stop(id); ok {
		t.Fatal("a stop found the command again")
	}
}

// UNIT-RELAY-09. Requirement: GATE-12.
func TestUNITRELAY09_HangUpBeforeTheStartCancels(t *testing.T) {
	r, h, id, call := opened()
	r.HangUp(id, call)
	if last := h.sent[len(h.sent)-1]; last != (messages.Message{Type: messages.TypeCancel, ID: id}) {
		t.Fatalf("sent %+v, want cancel", last)
	}
	r.FromHost(messages.Message{Type: messages.TypeRefused, ID: id, Reason: messages.ReasonAIGone})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledAIGone})
	wantNoAnswer(t, call)
	if r.held() != 0 {
		t.Fatal("the request is still held")
	}
}

// UNIT-RELAY-10. Requirement: LIM-07.
func TestUNITRELAY10_HangUpWhileItRunsCancels(t *testing.T) {
	r, h, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	r.HangUp(id, call)
	if got := h.types(); got != "[request cancel]" {
		t.Fatalf("sent %s, want request then cancel", got)
	}
	r.HangUp(id, call)
	if got := h.types(); got != "[request cancel]" {
		t.Fatalf("a second hang-up sent again: %s", got)
	}
}

// UNIT-RELAY-11. Requirement: LIM-03.
func TestUNITRELAY11_NoCancelAfterARunningAnswer(t *testing.T) {
	r, h, id, call := askedState()
	r.HangUp(id, call)
	if got := h.types(); got != "[request]" {
		t.Fatalf("sent %s, want the request only", got)
	}
	if s, ok := r.stateOf(id); !ok || s != asked {
		t.Fatal("the request is no longer Asked")
	}

	// A call that was answered and then replaced cancels nothing either.
	next, _ := r.Wait(id)
	r.HangUp(id, call)
	if got := h.types(); got != "[request wait]" {
		t.Fatalf("an old call cancelled: %s", got)
	}
	wantNoAnswer(t, next)
}

// UNIT-RELAY-12. Requirement: LIM-02.
func TestUNITRELAY12_WaitAndStopNeedAnAskedCommand(t *testing.T) {
	r, h, id, _ := opened()
	for _, try := range []func(string) (Call, bool){r.Wait, r.Stop} {
		if _, ok := try(id); ok {
			t.Fatal("a call was opened on a request that has one")
		}
		if _, ok := try("r_made_up"); ok {
			t.Fatal("a call was opened on a made-up id")
		}
		if _, ok := try(""); ok {
			t.Fatal("a call was opened on an empty id")
		}
	}
	if got := h.types(); got != "[request]" {
		t.Fatalf("sent %s, want the request only", got)
	}
}

// UNIT-RELAY-13. Requirement: GATE-06.
func TestUNITRELAY13_EachCallGetsItsOwnAnswer(t *testing.T) {
	r, _ := newRelay()
	first, callA := r.Ask("a")
	second, callB := r.Ask("b")
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: second, Text: "B"})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: second, How: messages.HowExit, ExitCode: code(2)})
	wantNoAnswer(t, callA)
	r.FromHost(messages.Message{Type: messages.TypeOutput, ID: first, Text: "A"})
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: first, How: messages.HowExit, ExitCode: code(1)})
	if a := got(t, callA); a.Output != "A" || *a.ExitCode != 1 {
		t.Fatalf("the first call got %+v", a)
	}
	if b := got(t, callB); b.Output != "B" || *b.ExitCode != 2 {
		t.Fatalf("the second call got %+v", b)
	}
}

// UNIT-RELAY-14. Requirement: KEY-03.
func TestUNITRELAY14_TheEndAnswersEveryOpenCall(t *testing.T) {
	r, h := newRelay()
	_, openCall := r.Ask("open")
	askedID, c := r.Ask("asked")
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: askedID})
	<-c
	doneID, c := r.Ask("done")
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: doneID})
	<-c
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: doneID, How: messages.HowExit, ExitCode: code(0)})
	before := len(h.sent)

	r.End()
	if a := got(t, openCall); a.State != StateRefused || a.Reason != messages.ReasonSessionEnded {
		t.Fatalf("the open call got %+v", a)
	}
	if r.held() != 0 {
		t.Fatal("requests are still held")
	}
	if _, ok := r.Wait(doneID); ok {
		t.Fatal("a kept result outlived the session")
	}
	id, call := r.Ask("late")
	if a := got(t, call); id != "" || a.State != StateRefused || a.Reason != messages.ReasonSessionEnded {
		t.Fatalf("an ask after the end got id %q and %+v", id, a)
	}
	if len(h.sent) != before {
		t.Fatalf("a message was sent after the end: %s", h.types())
	}
}

// UNIT-RELAY-15. Requirement: GATE-07.
func TestUNITRELAY15_OddMessagesFromTheHostAreDropped(t *testing.T) {
	r, h, id, call := opened()
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: "r_made_up", How: messages.HowExit})
	r.FromHost(messages.Message{Type: messages.TypeRefused, ID: "", Reason: messages.ReasonUser})
	r.FromHost(messages.Message{Type: "approve", ID: id})
	r.FromHost(messages.Message{Type: messages.TypeRequest, ID: id, Command: "rm -rf /"})
	r.FromHost(messages.Message{Type: messages.TypeSessionOK, ID: id})
	wantNoAnswer(t, call)
	if s, ok := r.stateOf(id); !ok || s != open {
		t.Fatal("the request is no longer Open")
	}
	if got := h.types(); got != "[request]" {
		t.Fatalf("sent %s, want the request only", got)
	}
}

// UNIT-RELAY-16. Requirement: GATE-06.
func TestUNITRELAY16_Counts(t *testing.T) {
	r, _ := newRelay()
	r.Ask("waits")
	runs, _ := r.Ask("runs")
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: runs})
	askedID, c := r.Ask("asked")
	r.FromHost(messages.Message{Type: messages.TypeStarted, ID: askedID})
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: askedID})
	<-c
	doneID, c := r.Ask("done")
	r.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: doneID})
	<-c
	r.FromHost(messages.Message{Type: messages.TypeEnded, ID: doneID, How: messages.HowExit, ExitCode: code(0)})

	waiting, running := r.Counts()
	if waiting != 1 {
		t.Fatalf("%d wait, want 1", waiting)
	}
	if running != runs && running != askedID {
		t.Fatalf("the running one is %q, want %q or %q", running, runs, askedID)
	}

	empty, _ := newRelay()
	if w, run := empty.Counts(); w != 0 || run != "" {
		t.Fatalf("an empty relay counts %d and %q", w, run)
	}
}

// UNIT-RELAY-17. Requirement: GATE-01.
func TestUNITRELAY17_ManyAtOnce(t *testing.T) {
	r, h := newRelay()
	const n = 200
	ids := make([]string, n)
	calls := make([]Call, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], calls[i] = r.Ask(fmt.Sprint("cmd ", i))
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("the id %s was made twice", id)
		}
		seen[id] = true
	}
	// The host answers each request with its own command as the output.
	h.mu.Lock()
	sent := append([]messages.Message(nil), h.sent...)
	h.mu.Unlock()
	for _, m := range sent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.FromHost(messages.Message{Type: messages.TypeOutput, ID: m.ID, Text: m.Command})
			r.FromHost(messages.Message{Type: messages.TypeEnded, ID: m.ID, How: messages.HowExit, ExitCode: code(0)})
		}()
	}
	wg.Wait()
	for i := range calls {
		if a := got(t, calls[i]); a.Output != fmt.Sprint("cmd ", i) {
			t.Fatalf("call %d got the output %q", i, a.Output)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-RELAY", "relay_test.go")
}
