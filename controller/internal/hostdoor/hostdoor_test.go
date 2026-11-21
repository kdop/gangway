package hostdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

const (
	keyK = "gw_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	keyL = "gw_fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	wait = 5 * time.Second
)

var limits = messages.Limits{CommandSeconds: 60, AnswerSeconds: 30, MaxWaiting: 20}

type fixture struct {
	t     *testing.T
	door  *Door
	store *sessions.Store
	url   string
}

func newFixture(t *testing.T, door *Door) *fixture {
	t.Helper()
	door.Store = sessions.NewStore()
	server := httptest.NewServer(door)
	t.Cleanup(server.Close)
	return &fixture{t: t, door: door, store: door.Store, url: "ws" + strings.TrimPrefix(server.URL, "http")}
}

// client is the cli-tool, played by the test.
type client struct {
	t    *testing.T
	conn *websocket.Conn
}

func (f *fixture) connect() *client {
	f.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	conn, _, err := websocket.Dial(ctx, f.url, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	conn.SetReadLimit(-1)
	f.t.Cleanup(func() { conn.CloseNow() })
	return &client{t: f.t, conn: conn}
}

func (c *client) send(msg messages.Message) {
	c.t.Helper()
	b, _ := json.Marshal(msg)
	c.sendRaw(b)
}

func (c *client) sendRaw(b []byte) {
	c.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

// read gives the next message. ok is false when the connection is closed.
func (c *client) read() (msg messages.Message, ok bool) {
	c.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	_, b, err := c.conn.Read(ctx)
	if err != nil {
		if ctx.Err() != nil {
			c.t.Fatal("no message and no close in time")
		}
		return msg, false
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		c.t.Fatal(err)
	}
	return msg, true
}

func (c *client) wantClosed() {
	c.t.Helper()
	if msg, ok := c.read(); ok {
		c.t.Fatalf("got %+v, want the connection closed", msg)
	}
}

func start(key string) messages.Message {
	l := limits
	return messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash(key), Host: "web-3", Limits: &l}
}

// live gives a client whose session is live, and the session.
func (f *fixture) live(key string) (*client, *sessions.Session) {
	f.t.Helper()
	c := f.connect()
	c.send(start(key))
	if msg, ok := c.read(); !ok || msg.Type != messages.TypeSessionOK {
		f.t.Fatalf("got %+v (ok=%v), want session_ok", msg, ok)
	}
	sess := f.store.Find(key)
	if sess == nil {
		f.t.Fatal("the session is not live after session_ok")
	}
	return c, sess
}

func (f *fixture) waitFor(what string, ok func() bool) {
	f.t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("timed out: %s", what)
}

func (f *fixture) wantNoSession() {
	f.t.Helper()
	f.waitFor("no session is live", func() bool { return f.store.Count() == 0 })
}

func answer(t *testing.T, call relay.Call) relay.Answer {
	t.Helper()
	select {
	case a := <-call:
		return a
	case <-time.After(wait):
		t.Fatal("the call got no answer in time")
		return relay.Answer{}
	}
}

func wantNoAnswer(t *testing.T, call relay.Call) {
	t.Helper()
	select {
	case a := <-call:
		t.Fatalf("the call got an answer: %+v", a)
	case <-time.After(100 * time.Millisecond):
	}
}

// UNIT-HOSTDOOR-01. Requirement: SES-01.
func TestUNITHOSTDOOR01_SessionStartMakesTheSessionLive(t *testing.T) {
	f := newFixture(t, &Door{})
	_, sess := f.live(keyK)
	if sess.Host != "web-3" || sess.Limits != limits || sess.KeyHash != messages.KeyHash(keyK) {
		t.Fatalf("the session is %+v", sess)
	}
}

// UNIT-HOSTDOOR-02. Requirement: SES-01.
func TestUNITHOSTDOOR02_TheFirstMessageMustBeSessionStart(t *testing.T) {
	f := newFixture(t, &Door{})

	c := f.connect()
	c.send(messages.Message{Type: messages.TypeStarted, ID: "r_1"})
	c.wantClosed()

	c = f.connect()
	other := start(keyK)
	other.Type = messages.TypeOutput
	c.send(other)
	c.wantClosed()

	c = f.connect()
	c.sendRaw([]byte("this is not JSON"))
	c.wantClosed()

	c = f.connect()
	c.send(messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash(keyK), Host: "web-3"})
	c.wantClosed()

	c = f.connect()
	bad := start(keyK)
	bad.KeyHash = ""
	c.send(bad)
	c.wantClosed()

	if f.store.Count() != 0 {
		t.Fatalf("%d sessions are live, want 0", f.store.Count())
	}
}

// UNIT-HOSTDOOR-03. Requirement: KEY-09.
func TestUNITHOSTDOOR03_ATakenHashIsRefused(t *testing.T) {
	f := newFixture(t, &Door{})
	first, sess := f.live(keyK)

	second := f.connect()
	second.send(start(keyK))
	second.wantClosed()

	if f.store.Find(keyK) != sess {
		t.Fatal("the first session is gone")
	}
	id, _ := sess.Relay.Ask("echo hi")
	if msg, ok := first.read(); !ok || msg.Type != messages.TypeRequest || msg.ID != id {
		t.Fatalf("the first host got %+v (ok=%v), want the request", msg, ok)
	}
}

// UNIT-HOSTDOOR-04. Requirement: KEY-03, SES-03.
func TestUNITHOSTDOOR04_ALostConnectionEndsTheSession(t *testing.T) {
	f := newFixture(t, &Door{})
	c, sess := f.live(keyK)
	_, call := sess.Relay.Ask("echo hi")
	c.read()

	c.conn.CloseNow()

	if a := answer(t, call); a.State != relay.StateRefused || a.Reason != messages.ReasonSessionEnded {
		t.Fatalf("the call got %+v", a)
	}
	// The key is dead before the call is answered.
	if f.store.Find(keyK) != nil {
		t.Fatal("the key still works after its call was answered")
	}
}

// UNIT-HOSTDOOR-05. Requirement: KEY-03.
func TestUNITHOSTDOOR05_SessionEndFromTheHost(t *testing.T) {
	f := newFixture(t, &Door{})
	c, sess := f.live(keyK)
	_, call := sess.Relay.Ask("echo hi")
	c.read()

	c.send(messages.Message{Type: messages.TypeSessionEnd, Reason: messages.EndUser})

	if a := answer(t, call); a.State != relay.StateRefused || a.Reason != messages.ReasonSessionEnded {
		t.Fatalf("the call got %+v", a)
	}
	if f.store.Find(keyK) != nil {
		t.Fatal("the key still works")
	}
	c.wantClosed()
}

// UNIT-HOSTDOOR-06. Requirement: GATE-01.
func TestUNITHOSTDOOR06_AnswersGoToTheRelay(t *testing.T) {
	f := newFixture(t, &Door{})
	c, sess := f.live(keyK)
	id, call := sess.Relay.Ask("echo hi")
	c.read()

	zero := 0
	c.send(messages.Message{Type: messages.TypeStarted, ID: id})
	c.send(messages.Message{Type: messages.TypeOutput, ID: id, Text: "hi\n"})
	c.send(messages.Message{Type: messages.TypeEnded, ID: id, How: messages.HowExit, ExitCode: &zero})

	a := answer(t, call)
	if a.State != relay.StateEnded || a.Output != "hi\n" || a.ExitCode == nil || *a.ExitCode != 0 {
		t.Fatalf("the call got %+v", a)
	}
}

// UNIT-HOSTDOOR-07. Requirement: GATE-07.
func TestUNITHOSTDOOR07_OtherTypesAreDropped(t *testing.T) {
	f := newFixture(t, &Door{})
	c, sess := f.live(keyK)
	id, call := sess.Relay.Ask("echo hi")
	c.read()

	c.send(messages.Message{Type: "approve", ID: id})
	c.send(messages.Message{Type: messages.TypeRequest, ID: id, Kind: messages.KindCommand, Command: "rm -rf /"})
	c.send(messages.Message{Type: messages.TypeSessionOK})
	c.send(start(keyL))
	c.sendRaw([]byte(`{"type":"extend","seconds":999999}`))

	wantNoAnswer(t, call)
	if f.store.Find(keyK) != sess || f.store.Count() != 1 {
		t.Fatal("the sessions changed")
	}
}

// UNIT-HOSTDOOR-08. Requirement: SES-01.
func TestUNITHOSTDOOR08_ASilentConnectionIsClosed(t *testing.T) {
	f := newFixture(t, &Door{StartWait: 50 * time.Millisecond})
	c := f.connect()
	c.wantClosed()
	if f.store.Count() != 0 {
		t.Fatal("a session is live")
	}
}

// UNIT-HOSTDOOR-09. Requirement: KEY-03.
func TestUNITHOSTDOOR09_ASlowHostIsCutOff(t *testing.T) {
	f := newFixture(t, &Door{OutQueue: 2})
	_, sess := f.live(keyK)
	big := strings.Repeat("x", 400_000)
	for i := 0; i < 400 && f.store.Count() > 0; i++ {
		sess.Relay.Ask(big)
	}
	f.wantNoSession()
}

// UNIT-HOSTDOOR-10. Requirement: SES-07.
func TestUNITHOSTDOOR10_ShuttingDownTellsTheHosts(t *testing.T) {
	f := newFixture(t, &Door{})
	a, _ := f.live(keyK)
	b, _ := f.live(keyL)

	f.door.EndAll(messages.EndControllerStopped)

	for _, c := range []*client{a, b} {
		msg, ok := c.read()
		if !ok || msg.Type != messages.TypeSessionEnd || msg.Reason != messages.EndControllerStopped {
			t.Fatalf("got %+v (ok=%v), want session_end with controller_stopped", msg, ok)
		}
	}
	f.wantNoSession()
}

// UNIT-HOSTDOOR-11. Requirement: CTL-01.
func TestUNITHOSTDOOR11_AWebPageIsRefused(t *testing.T) {
	f := newFixture(t, &Door{})
	ctx, stop := context.WithTimeout(context.Background(), wait)
	defer stop()
	conn, resp, err := websocket.Dial(ctx, f.url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		conn.CloseNow()
		t.Fatal("a connection from another web site was taken")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("the answer is %v, want 403", resp)
	}
	if f.store.Count() != 0 {
		t.Fatal("a session is live")
	}
}

// UNIT-HOSTDOOR-12. Requirement: KEY-03.
func TestUNITHOSTDOOR12_AMessageThatIsTooLargeEndsTheSession(t *testing.T) {
	f := newFixture(t, &Door{})
	c, _ := f.live(keyK)
	c.send(messages.Message{Type: messages.TypeOutput, ID: "r_1", Text: strings.Repeat("x", messages.MaxMessageBytes+1)})
	f.wantNoSession()
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-HOSTDOOR", "hostdoor_test.go")
}
