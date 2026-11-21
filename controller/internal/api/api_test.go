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

	mu   sync.Mutex
	sent []messages.Message
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, store: sessions.NewStore()}
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

// call makes one API call. auth is the whole Authorization header.
func (f *fixture) call(method, path, auth, body string) reply {
	f.t.Helper()
	return f.callCtx(context.Background(), method, path, auth, body)
}

func (f *fixture) callCtx(ctx context.Context, method, path, auth, body string) reply {
	f.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, f.url+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
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
	f.open("waits")
	runs, _ := f.open("runs")
	f.sess.Relay.FromHost(messages.Message{Type: messages.TypeStarted, ID: runs})

	hello := f.call("POST", "/v1/hello", "Bearer "+keyK, "")
	session := f.call("GET", "/v1/session", "Bearer "+keyK, "")
	if hello.status != http.StatusOK || session.status != http.StatusOK {
		t.Fatalf("hello gave %d, session gave %d", hello.status, session.status)
	}
	if hello.body != session.body {
		t.Fatalf("hello gave %s, session gave %s", hello.body, session.body)
	}
	want := `{"host":"web-3","limits":{"command_seconds":45,"answer_seconds":30},"requests":{"waiting":1,"max_waiting":7,"running":"` + runs + `"}}`
	if strings.TrimSpace(hello.body) != want {
		t.Fatalf("the body is %s, want %s", hello.body, want)
	}

	empty := newFixture(t)
	if r := empty.call("GET", "/v1/session", "Bearer "+keyK, ""); !strings.Contains(r.body, `"running":null`) {
		t.Fatalf("with nothing running the body is %s", r.body)
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

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-API", "api_test.go")
}
