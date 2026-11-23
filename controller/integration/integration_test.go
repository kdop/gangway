// Package integration runs the whole controller as architecture.md lays its
// integration tests out. The test plays both other sides: it calls the API
// as the AI, and it sits on a real websocket as the cli-tool.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/controller/internal/start"
	"github.com/kdop/gangway/shared/messages"
)

const (
	keyK = "gw_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	keyL = "gw_fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	wait = 5 * time.Second
)

var limits = messages.Limits{CommandSeconds: 45, AnswerSeconds: 30, MaxWaiting: 7}

type world struct {
	t   *testing.T
	c   *start.Controller
	url string

	mu     sync.Mutex
	tokens map[string]string // the caller token of each key that said hello
}

func newWorld(t *testing.T) *world {
	t.Helper()
	c := start.New()
	server := httptest.NewServer(c.Handler())
	t.Cleanup(server.Close)
	// Runs before the server closes: calls left open would hold it up.
	t.Cleanup(func() { c.Door.EndAll(messages.EndControllerStopped) })
	return &world{t: t, c: c, url: server.URL}
}

// host is the cli-tool, played by the test.
type host struct {
	t    *testing.T
	conn *websocket.Conn
}

// startSession connects a host and starts a session for this key.
func (w *world) startSession(key, name string) *host {
	w.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(w.url, "http")+messages.HostPath, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	conn.SetReadLimit(-1)
	w.t.Cleanup(func() { conn.CloseNow() })
	h := &host{t: w.t, conn: conn}
	l := limits
	h.send(messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash(key), Host: name, Limits: &l})
	if msg := h.read(); msg.Type != messages.TypeSessionOK {
		w.t.Fatalf("the host got %+v, want session_ok", msg)
	}
	return h
}

func (h *host) send(msg messages.Message) {
	h.t.Helper()
	b, _ := json.Marshal(msg)
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	if err := h.conn.Write(ctx, websocket.MessageText, b); err != nil {
		h.t.Fatal(err)
	}
}

func (h *host) read() messages.Message {
	h.t.Helper()
	msg, ok := h.tryRead(wait)
	if !ok {
		h.t.Fatal("the host got no message in time")
	}
	return msg
}

// tryRead gives the next message, or ok false when none comes in time.
func (h *host) tryRead(within time.Duration) (msg messages.Message, ok bool) {
	h.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), within)
	defer stop()
	_, b, err := h.conn.Read(ctx)
	if err != nil {
		return msg, false
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		h.t.Fatal(err)
	}
	return msg, true
}

// wantSilence fails when the host gets a message. The connection cannot be
// used after it: a read that timed out closes it.
func (h *host) wantSilence() {
	h.t.Helper()
	if msg, ok := h.tryRead(150 * time.Millisecond); ok {
		h.t.Fatalf("the host got %+v, want nothing", msg)
	}
}

type reply struct {
	status int
	body   string
	fields map[string]any
}

// hello says hello with the key and keeps the caller token it gets.
func (w *world) hello(key string) reply {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.callAs(context.Background(), "POST", "/v1/hello", key, "", "")
	if token, _ := r.fields["caller_token"].(string); token != "" {
		if w.tokens == nil {
			w.tokens = map[string]string{}
		}
		w.tokens[key] = token
	}
	return r
}

// token gives the caller token of the key. A key that has none yet says
// hello first, as an AI would.
func (w *world) token(key string) string {
	w.mu.Lock()
	token := w.tokens[key]
	w.mu.Unlock()
	if token == "" && key != "" {
		w.hello(key)
		w.mu.Lock()
		token = w.tokens[key]
		w.mu.Unlock()
	}
	return token
}

// call makes one API call as the AI that owns the key: the caller token
// goes along.
func (w *world) call(ctx context.Context, method, path, key, body string) reply {
	w.t.Helper()
	return w.callAs(ctx, method, path, key, w.token(key), body)
}

// callAs makes one API call with this key and this caller token, as they
// are. An empty one is not sent.
func (w *world) callAs(ctx context.Context, method, path, key, caller, body string) reply {
	w.t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, w.url+path, strings.NewReader(body))
	if err != nil {
		w.t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if caller != "" {
		req.Header.Set(messages.CallerHeader, caller)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return reply{}
		}
		w.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := reply{status: resp.StatusCode, body: string(b)}
	json.Unmarshal(b, &r.fields)
	return r
}

func (w *world) get(path, key string) reply {
	return w.call(context.Background(), "GET", path, key, "")
}

func (w *world) post(path, key, body string) reply {
	return w.call(context.Background(), "POST", path, key, body)
}

func commandBody(command string) string {
	b, _ := json.Marshal(map[string]string{"kind": "command", "command": command})
	return string(b)
}

// ask opens a request in the background. It gives the request the host got
// and a channel with the call's answer.
func (w *world) ask(h *host, key, command string) (messages.Message, chan reply) {
	w.t.Helper()
	done := make(chan reply, 1)
	go func() { done <- w.post("/v1/requests", key, commandBody(command)) }()
	msg := h.read()
	if msg.Type != messages.TypeRequest {
		w.t.Fatalf("the host got %+v, want a request", msg)
	}
	return msg, done
}

// later runs a call in the background.
func (w *world) later(path, key string) chan reply {
	done := make(chan reply, 1)
	go func() { done <- w.post(path, key, "") }()
	return done
}

func (w *world) result(done chan reply) reply {
	w.t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(wait):
		w.t.Fatal("the call did not end in time")
		return reply{}
	}
}

func code(n int) *int { return &n }

// running brings a request to the "still running" answer: API-10.
func (w *world) running(h *host, key string) (id string, answer reply) {
	w.t.Helper()
	req, done := w.ask(h, key, "sleep 300")
	h.send(messages.Message{Type: messages.TypeStarted, ID: req.ID})
	h.send(messages.Message{Type: messages.TypeOutput, ID: req.ID, Text: "so far\n"})
	h.send(messages.Message{Type: messages.TypeStillRunning, ID: req.ID})
	return req.ID, w.result(done)
}

func TestAPI01_Hello(t *testing.T) {
	w := newWorld(t)
	w.startSession(keyK, "web-3")
	r := w.hello(keyK)
	token, _ := r.fields["caller_token"].(string)
	want := `{"caller_token":"` + token + `","host":"web-3","limits":{"command_seconds":45,"answer_seconds":30},"requests":{"waiting":0,"max_waiting":7,"running":null}}`
	if r.status != http.StatusOK || !strings.HasPrefix(token, messages.CallerPrefix) || strings.TrimSpace(r.body) != want {
		t.Fatalf("hello gave %d %s, want %s", r.status, r.body, want)
	}
}

func TestAPI02_ASecondHelloIsRefused(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	token := w.token(keyK)
	for _, caller := range []string{"", token} {
		r := w.callAs(context.Background(), "POST", "/v1/hello", keyK, caller, "")
		if r.status != http.StatusUnauthorized || strings.Contains(r.body, "caller_token") {
			t.Fatalf("a second hello with the caller %q gave %d %s, want 401", caller, r.status, r.body)
		}
	}
	if r := w.get("/v1/session", keyK); r.status != http.StatusOK {
		t.Fatalf("the first caller got %d after the refused hellos, want 200", r.status)
	}
	h.wantSilence()
}

func TestAPI03_NoCallerTokenOrAWrongOne(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	other := w.startSession(keyL, "db-1")
	token := w.token(keyK)
	noKey := w.callAs(context.Background(), "GET", "/v1/session", "", "", "")
	for _, caller := range []string{"", token[:len(token)-1], token + "0", w.token(keyL), messages.CallerPrefix + strings.Repeat("0", 64)} {
		for _, c := range []struct{ method, path, body string }{
			{"GET", "/v1/session", ""},
			{"POST", "/v1/requests", commandBody("echo hi")},
			{"POST", "/v1/requests/r_1/wait", ""},
			{"POST", "/v1/requests/r_1/stop", ""},
		} {
			r := w.callAs(context.Background(), c.method, c.path, keyK, caller, c.body)
			if r.status != http.StatusUnauthorized || r.body != noKey.body {
				t.Errorf("%s %s with the caller %q gave %d %s, want the 401 of a call with no key", c.method, c.path, caller, r.status, r.body)
			}
		}
	}
	h.wantSilence()
	other.wantSilence()
}

func TestAPI04_SessionTellsWhatTheHostGave(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	other := w.startSession(keyL, "db-1")
	_ = other

	req, _ := w.ask(h, keyK, "sleep 300")
	h.send(messages.Message{Type: messages.TypeStarted, ID: req.ID})
	w.ask(h, keyK, "echo waits")
	// The answers travel on another connection than the API call, so wait
	// until the controller has seen the start.
	var r reply
	for i := 0; i < 500; i++ {
		r = w.get("/v1/session", keyK)
		if strings.Contains(r.body, req.ID) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	want := `{"host":"web-3","limits":{"command_seconds":45,"answer_seconds":30},"requests":{"waiting":1,"max_waiting":7,"running":"` + req.ID + `"}}`
	if r.status != http.StatusOK || strings.TrimSpace(r.body) != want {
		t.Fatalf("session gave %d %s, want %s", r.status, r.body, want)
	}
	if r := w.get("/v1/session", keyL); !strings.Contains(r.body, `"host":"db-1"`) || !strings.Contains(r.body, `"waiting":0`) {
		t.Fatalf("the other session gave %s", r.body)
	}
}

func TestAPI06_ARequestReachesTheHostLetterForLetter(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	command := "grep ERROR app.log |\n\ttail -n 20 # ✓ \\033[31m"
	req, _ := w.ask(h, keyK, command)
	want := messages.Message{Type: messages.TypeRequest, ID: req.ID, Kind: messages.KindCommand, Command: command}
	if req != want || req.ID == "" {
		t.Fatalf("the host got %+v, want %+v", req, want)
	}
	h.wantSilence()
}

func TestAPI07_RefusedWithEachReason(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	for _, reason := range []string{messages.ReasonUser, messages.ReasonControlCharacter, messages.ReasonLineFull} {
		req, done := w.ask(h, keyK, "echo hi")
		h.send(messages.Message{Type: messages.TypeRefused, ID: req.ID, Reason: reason})
		r := w.result(done)
		if r.status != http.StatusOK || r.fields["state"] != "refused" || r.fields["reason"] != reason {
			t.Fatalf("for %s the call gave %d %s", reason, r.status, r.body)
		}
	}
}

func TestAPI08_EndedWithTheOutputJoined(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	req, done := w.ask(h, keyK, "echo one; echo two")
	h.send(messages.Message{Type: messages.TypeStarted, ID: req.ID})
	h.send(messages.Message{Type: messages.TypeOutput, ID: req.ID, Text: "one\n"})
	h.send(messages.Message{Type: messages.TypeOutput, ID: req.ID, Text: "two\n"})
	h.send(messages.Message{Type: messages.TypeEnded, ID: req.ID, How: messages.HowExit, ExitCode: code(0)})
	r := w.result(done)
	if r.fields["state"] != "ended" || r.fields["how"] != "exit" || r.fields["exit_code"] != float64(0) || r.fields["output"] != "one\ntwo\n" {
		t.Fatalf("the call gave %s", r.body)
	}
}

func TestAPI09_CutAndBytesReplaced(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	req, done := w.ask(h, keyK, "cat big")
	h.send(messages.Message{Type: messages.TypeEnded, ID: req.ID, How: messages.HowExit, ExitCode: code(0), Cut: true, BytesReplaced: true})
	r := w.result(done)
	if r.fields["cut"] != true || r.fields["bytes_replaced"] != true {
		t.Fatalf("the call gave %s", r.body)
	}
}

func TestAPI10_StillRunning(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	id, r := w.running(h, keyK)
	if r.status != http.StatusOK || r.fields["state"] != "running" || r.fields["id"] != id ||
		r.fields["output"] != "so far\n" || r.fields["answer_within_seconds"] != float64(30) {
		t.Fatalf("the call gave %d %s", r.status, r.body)
	}
}

func TestAPI11_WaitGivesOnlyTheNewOutput(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	id, _ := w.running(h, keyK)
	done := w.later("/v1/requests/"+id+"/wait", keyK)
	if msg := h.read(); msg != (messages.Message{Type: messages.TypeWait, ID: id}) {
		t.Fatalf("the host got %+v, want wait", msg)
	}
	h.send(messages.Message{Type: messages.TypeOutput, ID: id, Text: "more\n"})
	h.send(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0)})
	r := w.result(done)
	if r.fields["state"] != "ended" || r.fields["output"] != "more\n" {
		t.Fatalf("the call gave %s", r.body)
	}
}

func TestAPI12_Stop(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	id, _ := w.running(h, keyK)
	done := w.later("/v1/requests/"+id+"/stop", keyK)
	if msg := h.read(); msg != (messages.Message{Type: messages.TypeStop, ID: id}) {
		t.Fatalf("the host got %+v, want stop", msg)
	}
	h.send(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowKilledByAI})
	r := w.result(done)
	if r.fields["state"] != "ended" || r.fields["how"] != "killed_by_ai" {
		t.Fatalf("the call gave %s", r.body)
	}
}

func TestAPI13_EndedBeforeTheWaitArrives(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	id, _ := w.running(h, keyK)
	h.send(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: code(0)})

	// The wait may arrive before the controller has read the end. Either
	// way the call must end with it, and the result is given once.
	r := w.post("/v1/requests/"+id+"/wait", keyK, "")
	if r.status != http.StatusOK || r.fields["state"] != "ended" || r.fields["how"] != "exit" {
		t.Fatalf("the call gave %d %s", r.status, r.body)
	}
	if again := w.post("/v1/requests/"+id+"/wait", keyK, ""); again.status != http.StatusNotFound {
		t.Fatalf("a second wait gave %d, want 404", again.status)
	}
}

func TestAPI14_AMadeUpIDAndAnotherSessionsID(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	other := w.startSession(keyL, "db-1")
	otherID, _ := w.running(other, keyL)

	for _, verb := range []string{"wait", "stop"} {
		for _, id := range []string{"r_made_up", otherID} {
			if r := w.post("/v1/requests/"+id+"/"+verb, keyK, ""); r.status != http.StatusNotFound {
				t.Errorf("%s on %s gave %d, want 404", verb, id, r.status)
			}
		}
	}
	// The other session's command is untouched: its owner can still stop it.
	done := w.later("/v1/requests/"+otherID+"/stop", keyL)
	if msg := other.read(); msg != (messages.Message{Type: messages.TypeStop, ID: otherID}) {
		t.Fatalf("the other host got %+v, want only its owner's stop", msg)
	}
	other.send(messages.Message{Type: messages.TypeEnded, ID: otherID, How: messages.HowKilledByAI})
	w.result(done)
	h.wantSilence()
	other.wantSilence()
}

func TestAPI15_HangingUpBeforeTheStartCancels(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	ctx, hangUp := context.WithCancel(context.Background())
	go w.call(ctx, "POST", "/v1/requests", keyK, commandBody("echo hi"))
	req := h.read()
	hangUp()
	if msg := h.read(); msg != (messages.Message{Type: messages.TypeCancel, ID: req.ID}) {
		t.Fatalf("the host got %+v, want cancel", msg)
	}
}

func TestAPI16_HangingUpWhileItRunsCancels(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	ctx, hangUp := context.WithCancel(context.Background())
	go w.call(ctx, "POST", "/v1/requests", keyK, commandBody("sleep 300"))
	req := h.read()
	h.send(messages.Message{Type: messages.TypeStarted, ID: req.ID})
	h.send(messages.Message{Type: messages.TypeOutput, ID: req.ID, Text: "x"})
	hangUp()
	if msg := h.read(); msg != (messages.Message{Type: messages.TypeCancel, ID: req.ID}) {
		t.Fatalf("the host got %+v, want cancel", msg)
	}
}

func TestAPI17_NoCancelAfterARunningAnswer(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	w.running(h, keyK)
	h.wantSilence()
}

func TestAPI18_NoKeyAWrongKeyAnEndedSession(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	ended := w.startSession(keyL, "db-1")
	ended.send(messages.Message{Type: messages.TypeSessionEnd, Reason: messages.EndUser})
	for i := 0; i < 500 && w.get("/v1/session", keyL).status == http.StatusOK; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	bodies := map[string]bool{}
	for _, key := range []string{"", "gw_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", keyK[:len(keyK)-1], keyK + "0", keyL} {
		for _, c := range []struct{ method, path, body string }{
			{"POST", "/v1/hello", ""},
			{"GET", "/v1/session", ""},
			{"POST", "/v1/requests", commandBody("echo hi")},
			{"POST", "/v1/requests/r_1/wait", ""},
			{"POST", "/v1/requests/r_1/stop", ""},
		} {
			r := w.call(context.Background(), c.method, c.path, key, c.body)
			if r.status != http.StatusUnauthorized {
				t.Errorf("%s %s with the key %q gave %d, want 401", c.method, c.path, key, r.status)
			}
			bodies[r.body] = true
		}
	}
	if len(bodies) != 1 {
		t.Errorf("the 401 answers differ: %v", bodies)
	}
	h.wantSilence()
}

func TestAPI21_TwoRequestsAnsweredOutOfOrder(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	first, firstDone := w.ask(h, keyK, "echo first")
	second, secondDone := w.ask(h, keyK, "echo second")
	h.send(messages.Message{Type: messages.TypeOutput, ID: second.ID, Text: "second\n"})
	h.send(messages.Message{Type: messages.TypeEnded, ID: second.ID, How: messages.HowExit, ExitCode: code(2)})
	if r := w.result(secondDone); r.fields["output"] != "second\n" || r.fields["exit_code"] != float64(2) {
		t.Fatalf("the second call gave %s", r.body)
	}
	h.send(messages.Message{Type: messages.TypeOutput, ID: first.ID, Text: "first\n"})
	h.send(messages.Message{Type: messages.TypeEnded, ID: first.ID, How: messages.HowExit, ExitCode: code(1)})
	if r := w.result(firstDone); r.fields["output"] != "first\n" || r.fields["exit_code"] != float64(1) {
		t.Fatalf("the first call gave %s", r.body)
	}
}

func TestAPI23_MadeUpPaths(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	req, _ := w.ask(h, keyK, "echo hi")
	for _, path := range []string{
		"/v1/requests/" + req.ID + "/approve",
		"/v1/requests/" + req.ID + "/yes",
		"/v1/requests/" + req.ID,
		"/v1/session/extend",
		"/v1/settings",
		"/v1/approve",
	} {
		r := w.post(path, keyK, `{"approved":true}`)
		if r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
			t.Errorf("%s gave %d, want 404 or 405", path, r.status)
		}
	}
	h.wantSilence()
}

func TestAPI24_SessionEndFromTheHost(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	_, done := w.ask(h, keyK, "echo hi")
	h.send(messages.Message{Type: messages.TypeSessionEnd, Reason: messages.EndUser})
	r := w.result(done)
	if r.status != http.StatusOK || r.fields["state"] != "refused" || r.fields["reason"] != "session_ended" {
		t.Fatalf("the call gave %d %s", r.status, r.body)
	}
	if r := w.get("/v1/session", keyK); r.status != http.StatusUnauthorized {
		t.Fatalf("the key gave %d after the end, want 401", r.status)
	}
}

// The MVP has no reconnect: a lost connection ends the session at once.
func TestMVP_ALostConnectionEndsTheSession(t *testing.T) {
	w := newWorld(t)
	h := w.startSession(keyK, "web-3")
	_, done := w.ask(h, keyK, "echo hi")
	h.conn.CloseNow()
	r := w.result(done)
	if r.fields["state"] != "refused" || r.fields["reason"] != "session_ended" {
		t.Fatalf("the call gave %d %s", r.status, r.body)
	}
	if r := w.get("/v1/session", keyK); r.status != http.StatusUnauthorized {
		t.Fatalf("the key gave %d after the loss, want 401", r.status)
	}
}

func TestPIE09_TheKeyWorksOnlyWhileTheSessionIsHeld(t *testing.T) {
	w := newWorld(t)
	if r := w.get("/v1/session", keyK); r.status != http.StatusUnauthorized {
		t.Fatalf("before the session the key gave %d, want 401", r.status)
	}
	h := w.startSession(keyK, "web-3")
	if r := w.get("/v1/session", keyK); r.status != http.StatusOK {
		t.Fatalf("with the session live the key gave %d, want 200", r.status)
	}
	h.conn.CloseNow()
	ok := false
	for i := 0; i < 500 && !ok; i++ {
		ok = w.get("/v1/session", keyK).status == http.StatusUnauthorized
		time.Sleep(10 * time.Millisecond)
	}
	if !ok {
		t.Fatal("the key still works after the session was dropped")
	}
}

// SES-07: a controller that is started again has no live session.
func TestSES07_ARestartHasNoSession(t *testing.T) {
	w := newWorld(t)
	w.startSession(keyK, "web-3")
	again := newWorld(t)
	if r := again.get("/v1/session", keyK); r.status != http.StatusUnauthorized {
		t.Fatalf("the new controller gave %d for the old key, want 401", r.status)
	}
}
