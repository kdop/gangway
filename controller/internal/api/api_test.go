package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

const (
	keyK = "gw_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	wait = 5 * time.Second
)

// fixture is the API over one live session. The test plays the host.
type fixture struct {
	t     *testing.T
	url   string
	store *sessions.Store
	sess  *sessions.Session

	// token is the caller token. call sends it when it is set.
	token string
	// hello is the answer to the first hello.
	hello reply

	mu   sync.Mutex
	sent []messages.Message
	now  time.Time // the controller's clock, in the test's hands
}

func (f *fixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fixture) pass(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// newFixture gives a live session whose key has been claimed by a hello.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := newUnclaimed(t)
	f.hello = f.call("POST", "/v1/hello", "Bearer "+keyK, "")
	f.token, _ = f.hello.fields["caller_token"].(string)
	if f.hello.status != http.StatusOK || f.token == "" {
		t.Fatalf("hello gave %d %s", f.hello.status, f.hello.body)
	}
	return f
}

// newUnclaimed gives a live session that no caller has said hello to.
func newUnclaimed(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, store: sessions.NewStore(), now: time.Unix(1000, 0)}
	f.store.Clock = f.clock
	f.sess = &sessions.Session{
		KeyHash: messages.KeyHash(keyK),
		Host:    "web-3",
		Limits:  messages.Limits{CommandSeconds: 45, AnswerSeconds: 30, MaxWaiting: 7},
		Relay: relay.New(func(m messages.Message) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sent = append(f.sent, m)
		}),
	}
	f.sess.Counts(f.clock(), 30*time.Minute)
	if err := f.store.Add(f.sess); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(f.store))
	t.Cleanup(server.Close)
	// Runs before the server closes: calls left open would hold it up.
	t.Cleanup(f.sess.Relay.End)
	f.url = server.URL
	return f
}

func (f *fixture) hostGot() []messages.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]messages.Message(nil), f.sent...)
}

// nextSent waits until the host has n messages and gives the last one.
func (f *fixture) nextSent(n int) messages.Message {
	f.t.Helper()
	for i := 0; i < 500; i++ {
		if got := f.hostGot(); len(got) >= n {
			return got[n-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("the host has %d messages, want %d", len(f.hostGot()), n)
	return messages.Message{}
}

func (f *fixture) wantNothingSent() {
	f.t.Helper()
	if got := f.hostGot(); len(got) != 0 {
		f.t.Fatalf("the host got %+v, want nothing", got)
	}
}

type reply struct {
	status int
	body   string
	fields map[string]any
}

// call makes one API call. auth is the whole Authorization header. The
// caller token of the fixture goes along.
func (f *fixture) call(method, path, auth, body string) reply {
	f.t.Helper()
	return f.callCtx(context.Background(), method, path, auth, body)
}

func (f *fixture) callCtx(ctx context.Context, method, path, auth, body string) reply {
	f.t.Helper()
	return f.do(ctx, method, path, auth, f.token, body)
}

// callAs makes one API call with this caller token. An empty one sends no
// such header.
func (f *fixture) callAs(method, path, auth, caller, body string) reply {
	f.t.Helper()
	return f.do(context.Background(), method, path, auth, caller, body)
}

func (f *fixture) do(ctx context.Context, method, path, auth, caller, body string) reply {
	f.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, f.url+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if caller != "" {
		req.Header.Set(messages.CallerHeader, caller)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return reply{}
		}
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := reply{status: resp.StatusCode, body: string(b)}
	json.Unmarshal(b, &r.fields)
	return r
}

// open starts a request in the background and gives its id once the host
// has it, and a channel with the call's answer.
func (f *fixture) open(command string) (string, chan reply) {
	f.t.Helper()
	before := len(f.hostGot())
	done := make(chan reply, 1)
	body, _ := json.Marshal(map[string]string{"kind": "command", "command": command})
	go func() { done <- f.call("POST", "/v1/requests", "Bearer "+keyK, string(body)) }()
	return f.nextSent(before + 1).ID, done
}

func (f *fixture) result(done chan reply) reply {
	f.t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(wait):
		f.t.Fatal("the call did not end in time")
		return reply{}
	}
}

func code(n int) *int { return &n }

var fiveCalls = []struct{ method, path string }{
	{"POST", "/v1/hello"},
	{"GET", "/v1/session"},
	{"POST", "/v1/requests"},
	{"POST", "/v1/requests/r_1/wait"},
	{"POST", "/v1/requests/r_1/stop"},
}

const goodBody = `{"kind":"command","command":"echo hi"}`

// UNIT-API-01. Requirement: KEY-02, SES-01.
func TestUNITAPI01_NoKeyNoEntry(t *testing.T) {
	f := newFixture(t)
	bodies := map[string]bool{}
	for _, c := range fiveCalls {
		for _, auth := range []string{
			"",
			"Bearer gw_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			"Bearer " + keyK[:len(keyK)-1],
			"Bearer " + keyK + "0",
			keyK,
			"Basic " + keyK,
			"bearer " + keyK,
			"Bearer  " + keyK,
			"Bearer " + messages.KeyHash(keyK),
		} {
			r := f.call(c.method, c.path, auth, goodBody)
			if r.status != http.StatusUnauthorized {
				t.Errorf("%s %s with %q gave %d, want 401", c.method, c.path, auth, r.status)
			}
			bodies[r.body] = true
		}
		r := f.call(c.method, c.path+"?key="+keyK+"&authorization=Bearer+"+keyK, "", goodBody)
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s %s with the key in the address gave %d, want 401", c.method, c.path, r.status)
		}
		bodies[r.body] = true
	}
	if len(bodies) != 1 {
		t.Errorf("the 401 answers differ: %v", bodies)
	}
	f.wantNothingSent()
}

// UNIT-API-02. Requirement: KEY-03.
func TestUNITAPI02_AnEndedSessionIsRefused(t *testing.T) {
	f := newFixture(t)
	if r := f.call("GET", "/v1/session", "Bearer "+keyK, ""); r.status != http.StatusOK {
		t.Fatalf("the live session gave %d, want 200", r.status)
	}
	f.store.Remove(f.sess)
	for _, c := range fiveCalls {
		if r := f.call(c.method, c.path, "Bearer "+keyK, goodBody); r.status != http.StatusUnauthorized {
			t.Errorf("%s %s gave %d, want 401", c.method, c.path, r.status)
		}
	}
	f.wantNothingSent()
}

// UNIT-API-03. Requirement: GATE-07.
func TestUNITAPI03_FiveCallsAndNoMore(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct{ method, path string }{
		{"POST", "/v1/requests/r_1/approve"},
		{"POST", "/v1/requests/r_1"},
		{"POST", "/v1/session/extend"},
		{"POST", "/v1/session"},
		{"PUT", "/v1/session"},
		{"POST", "/v1/settings"},
		{"GET", "/v1/requests"},
		{"GET", "/"},
		{"POST", "/v2/requests"},
		{"POST", "/v1/approve"},
	} {
		r := f.call(c.method, c.path, "Bearer "+keyK, goodBody)
		if r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s gave %d, want 404 or 405", c.method, c.path, r.status)
		}
	}
	f.wantNothingSent()
}

// UNIT-API-04. Requirement: CTL-11.
func TestUNITAPI04_ABodyItDoesNotKnow(t *testing.T) {
	f := newFixture(t)
	big, _ := json.Marshal(map[string]string{"kind": "command", "command": strings.Repeat("x", maxBody)})
	for _, body := range []string{
		"this is not JSON",
		"",
		`{"kind":"command"}`,
		`{"kind":"command","command":""}`,
		`{"kind":"read","path":"/etc/passwd"}`,
		`{"kind":"read","command":"echo hi"}`,
		`{"kind":"approve","command":"echo hi"}`,
		`{"command":"echo hi"}`,
		`{"kind":"command","command":7}`,
		`["command","echo hi"]`,
		string(big),
	} {
		r := f.call("POST", "/v1/requests", "Bearer "+keyK, body)
		if r.status != http.StatusBadRequest {
			shown := body
			if len(shown) > 60 {
				shown = shown[:60] + "…"
			}
			t.Errorf("the body %q gave %d, want 400", shown, r.status)
		}
	}
	f.wantNothingSent()
}

// UNIT-API-05. Requirement: GATE-07, GATE-05.
func TestUNITAPI05_OnlyTheCommandGoesOn(t *testing.T) {
	f := newFixture(t)
	command := "grep ERROR app.log |\n\ttail -n 20 # ✓"
	body, _ := json.Marshal(map[string]any{
		"kind": "command", "command": command,
		"approved": true, "id": "mine", "type": "started", "how": "exit",
	})
	go f.call("POST", "/v1/requests", "Bearer "+keyK, string(body))
	got := f.nextSent(1)
	want := messages.Message{Type: messages.TypeRequest, ID: got.ID, Kind: messages.KindCommand, Command: command}
	if got != want {
		t.Fatalf("the host got %+v, want %+v", got, want)
	}
	if got.ID == "mine" || !strings.HasPrefix(got.ID, "r_") {
		t.Fatalf("the id is %q, want one of the controller's own", got.ID)
	}
	raw, _ := json.Marshal(got)
	if bytes.Contains(raw, []byte("approved")) {
		t.Fatalf("the message holds the extra field: %s", raw)
	}
}

// UNIT-API-06. Requirement: GATE-02.
func TestUNITAPI06_Refused(t *testing.T) {
	f := newFixture(t)
	id, done := f.open("echo hi")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeRefused, ID: id, Reason: messages.ReasonUser})
	r := f.result(done)
	if r.status != http.StatusOK || len(r.fields) != 2 || r.fields["state"] != "refused" || r.fields["reason"] != "user" {
		t.Fatalf("the answer is %d %s", r.status, r.body)
	}
}

// UNIT-API-07. Requirement: GATE-01.
func TestUNITAPI07_Ended(t *testing.T) {
	f := newFixture(t)
	id, done := f.open("echo hi")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "hi\n"})
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0)})
	r := f.result(done)
	want := map[string]any{"state": "ended", "how": "exit", "exit_code": float64(0), "output": "hi\n", "cut": false, "bytes_replaced": false}
	if r.status != http.StatusOK || len(r.fields) != len(want) {
		t.Fatalf("the answer is %d %s", r.status, r.body)
	}
	for k, v := range want {
		if r.fields[k] != v {
			t.Errorf("%s is %v, want %v", k, r.fields[k], v)
		}
	}
}

// UNIT-API-08. Requirement: LIM-01.
func TestUNITAPI08_Running(t *testing.T) {
	f := newFixture(t)
	id, done := f.open("sleep 300")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStarted, ID: id})
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeOutput, ID: id, Text: "so far\n"})
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: id})
	r := f.result(done)
	want := map[string]any{"state": "running", "id": id, "output": "so far\n", "answer_within_seconds": float64(30)}
	if r.status != http.StatusOK || len(r.fields) != len(want) {
		t.Fatalf("the answer is %d %s", r.status, r.body)
	}
	for k, v := range want {
		if r.fields[k] != v {
			t.Errorf("%s is %v, want %v", k, r.fields[k], v)
		}
	}
}

// UNIT-API-09. Requirement: LIM-02.
func TestUNITAPI09_WaitAndStop(t *testing.T) {
	f := newFixture(t)
	for i, verb := range []string{"wait", "stop"} {
		id, done := f.open("sleep 300")
		f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStillRunning, ID: id})
		f.result(done)

		next := make(chan reply, 1)
		go func() { next <- f.call("POST", "/v1/requests/"+id+"/"+verb, "Bearer "+keyK, "") }()
		if got := f.nextSent(2*i + 2); got != (messages.Message{Type: verb, ID: id}) {
			t.Fatalf("the host got %+v, want %s", got, verb)
		}
		f.sess.Relay.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledByAI})
		if r := f.result(next); r.status != http.StatusOK || r.fields["state"] != "ended" {
			t.Fatalf("%s ended with %d %s", verb, r.status, r.body)
		}
	}
}

// UNIT-API-10. Requirement: LIM-02.
func TestUNITAPI10_AMadeUpID(t *testing.T) {
	f := newFixture(t)
	for _, verb := range []string{"wait", "stop"} {
		if r := f.call("POST", "/v1/requests/r_made_up/"+verb, "Bearer "+keyK, ""); r.status != http.StatusNotFound {
			t.Errorf("%s gave %d, want 404", verb, r.status)
		}
	}
	f.wantNothingSent()

	// A request that has a call open is not one that waits for an answer.
	id, _ := f.open("echo hi")
	for _, verb := range []string{"wait", "stop"} {
		if r := f.call("POST", "/v1/requests/"+id+"/"+verb, "Bearer "+keyK, ""); r.status != http.StatusNotFound {
			t.Errorf("%s on an open request gave %d, want 404", verb, r.status)
		}
	}
	if got := f.hostGot(); len(got) != 1 {
		t.Fatalf("the host got %+v, want the request only", got)
	}
}

// UNIT-API-11. Requirement: GATE-12, LIM-07.
func TestUNITAPI11_HangingUpCancels(t *testing.T) {
	f := newFixture(t)
	ctx, hangUp := context.WithCancel(context.Background())
	body := goodBody
	go f.callCtx(ctx, "POST", "/v1/requests", "Bearer "+keyK, body)
	id := f.nextSent(1).ID
	hangUp()
	if got := f.nextSent(2); got != (messages.Message{Type: messages.TypeCancel, ID: id}) {
		t.Fatalf("the host got %+v, want cancel", got)
	}
}

// UNIT-API-12. Requirement: GATE-06.
func TestUNITAPI12_HelloAndSession(t *testing.T) {
	f := newFixture(t)
	empty := `{"host":"web-3","seconds_left":1800,"time_up":false,"limits":{"command_seconds":45,"answer_seconds":30},"requests":{"waiting":0,"max_waiting":7,"running":null}}`
	session := f.call("GET", "/v1/session", "Bearer "+keyK, "")
	if session.status != http.StatusOK || strings.TrimSpace(session.body) != empty {
		t.Fatalf("session gave %d %s, want %s", session.status, session.body, empty)
	}
	if want := `{"caller_token":"` + f.token + `",` + empty[1:]; strings.TrimSpace(f.hello.body) != want {
		t.Fatalf("hello gave %s, want %s", f.hello.body, want)
	}

	f.open("waits")
	runs, _ := f.open("runs")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStarted, ID: runs})
	want := `{"host":"web-3","seconds_left":1800,"time_up":false,"limits":{"command_seconds":45,"answer_seconds":30},"requests":{"waiting":1,"max_waiting":7,"running":"` + runs + `"}}`
	if r := f.call("GET", "/v1/session", "Bearer "+keyK, ""); strings.TrimSpace(r.body) != want {
		t.Fatalf("the body is %s, want %s", r.body, want)
	}
}

// UNIT-API-13. Requirement: LIM-08.
func TestUNITAPI13_NoExitCodeForAKilledCommand(t *testing.T) {
	f := newFixture(t)
	id, done := f.open("sleep 300")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledByUser})
	r := f.result(done)
	if _, has := r.fields["exit_code"]; has || r.fields["how"] != "killed_by_user" {
		t.Fatalf("the answer is %s", r.body)
	}
}

var otherCalls = fiveCalls[1:]

// let tells whether the call got past the key and the caller token.
func let(r reply) bool { return r.status != http.StatusUnauthorized }

// UNIT-API-14. Requirement: KEY-05.
func TestUNITAPI14_TheFirstHelloGetsTheCallerToken(t *testing.T) {
	f := newUnclaimed(t)
	r := f.call("POST", "/v1/hello", "Bearer "+keyK, "")
	token, _ := r.fields["caller_token"].(string)
	if r.status != http.StatusOK || !strings.HasPrefix(token, messages.CallerPrefix) {
		t.Fatalf("the first hello gave %d %s, want 200 with a caller token", r.status, r.body)
	}
	if !f.sess.Owns(token) {
		t.Fatal("the token that hello gave is not the one the session knows")
	}
	if r.fields["host"] != "web-3" {
		t.Fatalf("hello gave no state of the session: %s", r.body)
	}
}

// UNIT-API-15. Requirement: KEY-05.
func TestUNITAPI15_ASecondHelloIsRefused(t *testing.T) {
	f := newFixture(t)
	noKey := f.callAs("POST", "/v1/hello", "", "", "")
	for _, caller := range []string{"", f.token, messages.CallerPrefix + strings.Repeat("0", 64)} {
		r := f.callAs("POST", "/v1/hello", "Bearer "+keyK, caller, "")
		if r.status != http.StatusUnauthorized || r.body != noKey.body {
			t.Errorf("a second hello with the caller %q gave %d %s, want the 401 of a call with no key", caller, r.status, r.body)
		}
		if strings.Contains(r.body, "caller_token") {
			t.Errorf("a second hello handed out a token: %s", r.body)
		}
	}
	// The first caller is still the owner.
	if r := f.call("GET", "/v1/session", "Bearer "+keyK, ""); r.status != http.StatusOK {
		t.Fatalf("after the refused hellos the owner got %d, want 200", r.status)
	}
}

// UNIT-API-16. Requirement: KEY-05.
func TestUNITAPI16_NoTokenOrAWrongOneIsRefused(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	noKey := f.callAs("GET", "/v1/session", "", "", "")
	for _, c := range otherCalls {
		for _, caller := range []string{
			"",
			f.token[:len(f.token)-1],
			f.token + "0",
			strings.TrimPrefix(f.token, messages.CallerPrefix),
			strings.ToUpper(f.token),
			other.token,
			keyK,
			messages.KeyHash(f.token),
		} {
			r := f.callAs(c.method, c.path, "Bearer "+keyK, caller, goodBody)
			if r.status != http.StatusUnauthorized || r.body != noKey.body {
				t.Errorf("%s %s with the caller %q gave %d %s, want the 401 of a call with no key", c.method, c.path, caller, r.status, r.body)
			}
		}
		// The token in the address is no token.
		r := f.callAs(c.method, c.path+"?caller_token="+f.token+"&Gangway-Caller="+f.token, "Bearer "+keyK, "", goodBody)
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s %s with the token in the address gave %d, want 401", c.method, c.path, r.status)
		}
	}
	f.wantNothingSent()
}

// UNIT-API-17. Requirement: KEY-05.
func TestUNITAPI17_BeforeHelloOnlyHelloIsLetIn(t *testing.T) {
	f := newUnclaimed(t)
	for _, c := range otherCalls {
		for _, caller := range []string{"", messages.CallerPrefix + strings.Repeat("0", 64)} {
			if r := f.callAs(c.method, c.path, "Bearer "+keyK, caller, goodBody); let(r) {
				t.Errorf("%s %s before any hello gave %d, want 401", c.method, c.path, r.status)
			}
		}
	}
	f.wantNothingSent()
	// The refused calls took nothing: the first hello still gets the token.
	r := f.call("POST", "/v1/hello", "Bearer "+keyK, "")
	if token, _ := r.fields["caller_token"].(string); r.status != http.StatusOK || token == "" {
		t.Fatalf("hello after the refused calls gave %d %s, want a caller token", r.status, r.body)
	}
}

// UNIT-API-18. Requirement: KEY-05.
func TestUNITAPI18_TheKeyAndTheTokenAreLetIn(t *testing.T) {
	f := newFixture(t)
	if r := f.callAs("GET", "/v1/session", "Bearer "+keyK, f.token, ""); r.status != http.StatusOK {
		t.Fatalf("session with the key and the token gave %d, want 200", r.status)
	}
	// wait and stop on a made-up id are let in, and then not found.
	for _, c := range otherCalls[2:] {
		if r := f.callAs(c.method, c.path, "Bearer "+keyK, f.token, ""); r.status != http.StatusNotFound {
			t.Fatalf("%s %s with the key and the token gave %d, want 404", c.method, c.path, r.status)
		}
	}
	done := make(chan reply, 1)
	go func() { done <- f.callAs("POST", "/v1/requests", "Bearer "+keyK, f.token, goodBody) }()
	if got := f.nextSent(1); got.Command != "echo hi" {
		t.Fatalf("the host got %+v, want the request", got)
	}
}

// UNIT-API-19. Requirement: KEY-05.
func TestUNITAPI19_ManyFirstHellosAtOnceGiveOneToken(t *testing.T) {
	f := newUnclaimed(t)
	const callers = 40
	replies := make(chan reply, callers)
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			replies <- f.call("POST", "/v1/hello", "Bearer "+keyK, "")
		}()
	}
	close(start)
	tokens := 0
	for i := 0; i < callers; i++ {
		r := <-replies
		switch {
		case r.status == http.StatusOK && r.fields["caller_token"] != nil:
			tokens++
		case r.status != http.StatusUnauthorized:
			t.Errorf("a hello gave %d %s", r.status, r.body)
		}
	}
	if tokens != 1 {
		t.Fatalf("%d callers got a token, want exactly one", tokens)
	}
}

// wantTime checks what a call tells about the session's time.
func wantTime(t *testing.T, r reply, seconds float64, up bool) {
	t.Helper()
	if r.status != http.StatusOK || r.fields["seconds_left"] != seconds || r.fields["time_up"] != up {
		t.Fatalf("got %d %s, want seconds_left %v and time_up %v", r.status, r.body, seconds, up)
	}
}

// UNIT-API-20. Requirement: LEN-01, LEN-06.
func TestUNITAPI20_TheTimeLeftAndTimeUp(t *testing.T) {
	f := newFixture(t)
	wantTime(t, f.hello, 1800, false)
	f.pass(10 * time.Minute)
	wantTime(t, f.call("GET", "/v1/session", "Bearer "+keyK, ""), 1200, false)
	f.sess.TimeIsUp()
	wantTime(t, f.call("GET", "/v1/session", "Bearer "+keyK, ""), 0, true)
	f.sess.Extended(f.clock(), 600*time.Second)
	wantTime(t, f.call("GET", "/v1/session", "Bearer "+keyK, ""), 600, false)
}

// UNIT-API-21. Requirement: LEN-09.
func TestUNITAPI21_NoCallMovesTheTime(t *testing.T) {
	f := newFixture(t)
	f.pass(10 * time.Minute)
	wantTime(t, f.call("GET", "/v1/session", "Bearer "+keyK, ""), 1200, false)

	more := `{"seconds_left":3600,"extend":true,"length_seconds":3600,"time_up":false}`
	for _, c := range []struct{ method, path string }{
		{"POST", "/v1/session/extend"},
		{"POST", "/v1/extend"},
		{"POST", "/v1/session"},
		{"PUT", "/v1/session"},
		{"PATCH", "/v1/session"},
		{"POST", "/v1/session?seconds_left=3600"},
	} {
		r := f.call(c.method, c.path, "Bearer "+keyK, more)
		if r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s gave %d, want 404 or 405", c.method, c.path, r.status)
		}
	}
	if r := f.call("POST", "/v1/hello", "Bearer "+keyK, more); r.status != http.StatusUnauthorized {
		t.Errorf("a second hello that asks for more time gave %d, want 401", r.status)
	}
	// A GET that carries the wish in a body, in the address and in a
	// header answers as if nothing was asked.
	req, err := http.NewRequest("GET", f.url+"/v1/session?seconds_left=3600&extend=1", strings.NewReader(more))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+keyK)
	req.Header.Set(messages.CallerHeader, f.token)
	req.Header.Set("Gangway-Extend", "3600")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	got := reply{status: resp.StatusCode, body: string(b)}
	json.Unmarshal(b, &got.fields)
	wantTime(t, got, 1200, false)

	f.wantNothingSent()
	wantTime(t, f.call("GET", "/v1/session", "Bearer "+keyK, ""), 1200, false)
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-API", "api_test.go")
}
