package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

// The gate and the runner send through the connection.
var (
	_ gate.Sender   = (*Conn)(nil)
	_ runner.Sender = (*Conn)(nil)
)

const patience = 5 * time.Second

var startA = Start{
	KeyHash: messages.KeyHash("gw_test"),
	Host:    "web-3",
	Limits:  messages.Limits{CommandSeconds: 60, AnswerSeconds: 30, MaxWaiting: 20},
}

// recorder is the loop's side: it notes each event as one line of text.
type recorder struct{ events chan string }

func newRecorder() *recorder { return &recorder{events: make(chan string, 10000)} }

func (r *recorder) Request(req request.Request) {
	r.events <- "request " + req.ID + " " + req.Command
}
func (r *recorder) Wait(id string)       { r.events <- "wait " + id }
func (r *recorder) Stop(id string)       { r.events <- "stop " + id }
func (r *recorder) Cancel(id string)     { r.events <- "cancel " + id }
func (r *recorder) SessionEnd(re string) { r.events <- "session_end " + re }
func (r *recorder) Lost(err error)       { r.events <- "lost " + err.Error() }

func (r *recorder) next(t *testing.T) string {
	t.Helper()
	select {
	case e := <-r.events:
		return e
	case <-time.After(patience):
		t.Fatal("no event came")
		return ""
	}
}

func (r *recorder) want(t *testing.T, want string) {
	t.Helper()
	if got := r.next(t); got != want {
		t.Fatalf("the event is %q, want %q", got, want)
	}
}

// none checks that no event comes for a short while.
func (r *recorder) none(t *testing.T) {
	t.Helper()
	select {
	case e := <-r.events:
		t.Fatalf("an event came: %q", e)
	case <-time.After(150 * time.Millisecond):
	}
}

// controller is a web server inside the test that speaks websocket.
type controller struct {
	srv   *httptest.Server
	conns chan *websocket.Conn
	paths chan string
}

func newController(t *testing.T) *controller {
	t.Helper()
	c := &controller{conns: make(chan *websocket.Conn, 10), paths: make(chan string, 10)}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ws.SetReadLimit(64 << 20)
		c.paths <- r.URL.Path
		c.conns <- ws
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// accept gives the controller's end of the next connection.
func (c *controller) accept(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case ws := <-c.conns:
		t.Cleanup(func() { ws.CloseNow() })
		return ws
	case <-time.After(patience):
		t.Fatal("the cli-tool did not connect")
		return nil
	}
}

// readRaw gives the next message the controller got, as it came.
func readRaw(t *testing.T, ws *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("the controller got no message: %v", err)
	}
	return string(data)
}

func read(t *testing.T, ws *websocket.Conn) messages.Message {
	t.Helper()
	var m messages.Message
	raw := readRaw(t, ws)
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("the controller got %q, which is not JSON", raw)
	}
	return m
}

func write(t *testing.T, ws *websocket.Conn, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		t.Fatalf("the controller could not send: %v", err)
	}
}

// wantClosed checks that the controller sees the connection end.
func wantClosed(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	if _, data, err := ws.Read(ctx); err == nil {
		t.Fatalf("the controller got %q, want the connection to be closed", data)
	} else if ctx.Err() != nil {
		t.Fatal("the connection was not closed")
	}
}

type dialed struct {
	conn    *Conn
	err     error
	address string
}

// dialAsync dials in the background, so the test can play the controller.
func dialAsync(ctx context.Context, c *controller, h Handler) chan dialed {
	done := make(chan dialed, 1)
	go func() {
		conn, address, err := Dial(ctx, http.DefaultClient, c.srv.URL, startA, h)
		done <- dialed{conn, err, address}
	}()
	return done
}

func await(t *testing.T, done chan dialed) dialed {
	t.Helper()
	select {
	case d := <-done:
		return d
	case <-time.After(patience):
		t.Fatal("the dial did not come back")
		return dialed{}
	}
}

// live gives a connection in the Live state, the controller's end of it,
// and the recorder of its events.
func live(t *testing.T) (*Conn, *websocket.Conn, *recorder) {
	t.Helper()
	c := newController(t)
	rec := newRecorder()
	done := dialAsync(context.Background(), c, rec)
	ws := c.accept(t)
	if m := read(t, ws); m.Type != messages.TypeSessionStart {
		t.Fatalf("the first message is %q, want session_start", m.Type)
	}
	write(t, ws, `{"type":"session_ok"}`)
	d := await(t, done)
	if d.err != nil {
		t.Fatal(d.err)
	}
	t.Cleanup(func() {
		ws.CloseNow()
		d.conn.Close("test")
	})
	return d.conn, ws, rec
}

// UNIT-CONNECTION-01. Requirement: SES-06.
func TestUNITCONNECTION01_DialStartsTheSession(t *testing.T) {
	c := newController(t)
	rec := newRecorder()
	done := dialAsync(context.Background(), c, rec)
	ws := c.accept(t)
	if path := <-c.paths; path != "/host/v1" {
		t.Fatalf("the path is %q, want /host/v1", path)
	}
	raw := readRaw(t, ws)
	if strings.Contains(raw, messages.KeyPrefix) {
		t.Fatalf("session_start holds a key: %s", raw)
	}
	var m messages.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != messages.TypeSessionStart || m.KeyHash != startA.KeyHash || m.Host != "web-3" || m.Limits == nil || *m.Limits != startA.Limits {
		t.Fatalf("session_start is %s", raw)
	}
	select {
	case d := <-done:
		t.Fatalf("the dial came back before session_ok: %v", d.err)
	case <-time.After(100 * time.Millisecond):
	}
	write(t, ws, `{"type":"session_ok"}`)
	d := await(t, done)
	if d.err != nil {
		t.Fatal(d.err)
	}
	rec.none(t)
	ws.CloseNow()
	d.conn.Close("test")
}

// UNIT-CONNECTION-02. Requirement: SES-01.
func TestUNITCONNECTION02_NoSessionWithoutSessionOK(t *testing.T) {
	answers := []func(ctx context.Context, ws *websocket.Conn) error{
		func(ctx context.Context, ws *websocket.Conn) error {
			return ws.Write(ctx, websocket.MessageText, []byte(`{"type":"request","id":"r1","kind":"command","command":"ls"}`))
		},
		func(ctx context.Context, ws *websocket.Conn) error {
			return ws.Write(ctx, websocket.MessageText, []byte(`session_ok`))
		},
		func(ctx context.Context, ws *websocket.Conn) error {
			return ws.Write(ctx, websocket.MessageBinary, []byte(`{"type":"session_ok"}`))
		},
	}
	for i, answer := range answers {
		c := newController(t)
		rec := newRecorder()
		done := dialAsync(context.Background(), c, rec)
		ws := c.accept(t)
		read(t, ws)
		if err := answer(context.Background(), ws); err != nil {
			t.Fatal(err)
		}
		if d := await(t, done); d.err == nil || d.conn != nil {
			t.Fatalf("answer %d: the dial made a session", i)
		}
		rec.none(t)
	}
}

// UNIT-CONNECTION-03. Requirement: SES-01.
func TestUNITCONNECTION03_AControllerThatDoesNotAnswer(t *testing.T) {
	// It closes with no answer.
	c := newController(t)
	done := dialAsync(context.Background(), c, newRecorder())
	ws := c.accept(t)
	read(t, ws)
	ws.CloseNow()
	if d := await(t, done); d.err == nil {
		t.Fatal("a controller that closed made a session")
	}

	// It never answers.
	c = newController(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	began := time.Now()
	done = dialAsync(ctx, c, newRecorder())
	read(t, c.accept(t))
	if d := await(t, done); d.err == nil {
		t.Fatal("a silent controller made a session")
	}
	if took := time.Since(began); took < 150*time.Millisecond || took > 3*time.Second {
		t.Fatalf("the dial gave up after %v, want about 200ms", took)
	}

	// It is not there.
	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()
	if conn, _, err := Dial(context.Background(), http.DefaultClient, url, startA, newRecorder()); err == nil || conn != nil {
		t.Fatal("a dial to nothing made a session")
	}
}

// UNIT-CONNECTION-04. Requirement: GATE-05.
func TestUNITCONNECTION04_ARequestComesThroughAsItWas(t *testing.T) {
	_, ws, rec := live(t)
	command := "  cat > 'a b.txt' <<\"END\"\n\tκαλημέρα \\n \"x\"\nEND  "
	data, _ := json.Marshal(messages.Message{Type: messages.TypeRequest, ID: "r_81c2", Kind: messages.KindCommand, Command: command})
	write(t, ws, string(data))
	rec.want(t, "request r_81c2 "+command)
	rec.none(t)
}

// UNIT-CONNECTION-05. Requirement: GATE-07.
func TestUNITCONNECTION05_NoMessageApproves(t *testing.T) {
	_, ws, rec := live(t)
	for _, kind := range []string{"approve", "approved", "yes", "extend", "settings", "started", "ended", "no_such_type", ""} {
		write(t, ws, fmt.Sprintf(`{"type":%q,"id":"r1","kind":"command","command":"rm -rf x","approved":true,"answer":"y","seconds":9999}`, kind))
	}
	rec.none(t)
	write(t, ws, `{"type":"request","id":"r2","kind":"command","command":"ls","approved":true,"answer":"y"}`)
	rec.want(t, "request r2 ls")
	rec.none(t)
}

// UNIT-CONNECTION-06. Requirement: LIM-02.
func TestUNITCONNECTION06_WaitAndStop(t *testing.T) {
	_, ws, rec := live(t)
	write(t, ws, `{"type":"wait"}`)
	write(t, ws, `{"type":"stop"}`)
	write(t, ws, `{"type":"wait","id":"r1"}`)
	write(t, ws, `{"type":"stop","id":"r1"}`)
	rec.want(t, "wait r1")
	rec.want(t, "stop r1")
	rec.none(t)
}

// UNIT-CONNECTION-07. Requirement: GATE-12, LIM-07.
func TestUNITCONNECTION07_Cancel(t *testing.T) {
	_, ws, rec := live(t)
	write(t, ws, `{"type":"cancel"}`)
	write(t, ws, `{"type":"cancel","id":"r7"}`)
	rec.want(t, "cancel r7")
	rec.none(t)
}

// UNIT-CONNECTION-08. Requirement: GATE-01, GATE-02.
func TestUNITCONNECTION08_StartedAndRefused(t *testing.T) {
	conn, ws, _ := live(t)
	conn.Started("a")
	conn.Refused("b", messages.ReasonUser)
	if got := readRaw(t, ws); got != `{"type":"started","id":"a"}` {
		t.Fatalf("the controller got %s", got)
	}
	if got := readRaw(t, ws); got != `{"type":"refused","id":"b","reason":"user"}` {
		t.Fatalf("the controller got %s", got)
	}
}

// UNIT-CONNECTION-09. Requirement: LIM-01, SHOW-03.
func TestUNITCONNECTION09_OutputStillRunningAndEnded(t *testing.T) {
	conn, ws, _ := live(t)
	conn.Output("a", "one\n")
	conn.StillRunning("a")
	conn.Ended("a", runner.Result{How: messages.HowExit, ExitCode: 3})
	conn.Ended("b", runner.Result{How: messages.HowExit, ExitCode: 0})
	conn.Ended("c", runner.Result{How: messages.HowKilledByUser, ExitCode: 143, BytesReplaced: true})
	want := []string{
		`{"type":"output","id":"a","text":"one\n"}`,
		`{"type":"still_running","id":"a"}`,
		`{"type":"ended","id":"a","how":"exit","exit_code":3}`,
		`{"type":"ended","id":"b","how":"exit","exit_code":0}`,
		`{"type":"ended","id":"c","how":"killed_by_user","bytes_replaced":true}`,
	}
	for _, w := range want {
		if got := readRaw(t, ws); got != w {
			t.Fatalf("the controller got %s, want %s", got, w)
		}
	}
}

// UNIT-CONNECTION-10. Requirement: SHOW-03.
func TestUNITCONNECTION10_OutputIsNotChanged(t *testing.T) {
	conn, ws, _ := live(t)
	text := "\x1b[31mred\x1b[0m \x00 \"q\" \\ καλημέρα �\r\n\t<&>"
	conn.Output("a", text)
	if m := read(t, ws); m.Text != text {
		t.Fatalf("the controller got %q, want %q", m.Text, text)
	}
}

// UNIT-CONNECTION-11. Requirement: SHOW-02.
func TestUNITCONNECTION11_MessagesKeepTheirOrder(t *testing.T) {
	conn, ws, _ := live(t)
	const n = 2000
	for i := 0; i < n; i++ {
		conn.Output("a", fmt.Sprint(i))
	}
	for i := 0; i < n; i++ {
		if m := read(t, ws); m.Text != fmt.Sprint(i) {
			t.Fatalf("message %d holds %q", i, m.Text)
		}
	}
}

// UNIT-CONNECTION-12. Requirement: SES-05, SES-07.
func TestUNITCONNECTION12_ALostConnectionIsToldOnce(t *testing.T) {
	conn, ws, rec := live(t)
	ws.CloseNow()
	if got := rec.next(t); !strings.HasPrefix(got, "lost ") {
		t.Fatalf("the event is %q, want lost", got)
	}
	began := time.Now()
	for i := 0; i < 100; i++ {
		conn.Output("a", "x")
		conn.Started("a")
		conn.Ended("a", runner.Result{How: messages.HowExit})
	}
	conn.Close("user")
	conn.Close("user")
	conn.Refused("a", messages.ReasonUser)
	if took := time.Since(began); took > time.Second {
		t.Fatalf("sending and closing after the loss took %v", took)
	}
	rec.none(t)
}

// UNIT-CONNECTION-13. Requirement: SES-02.
func TestUNITCONNECTION13_CloseSendsSessionEndLast(t *testing.T) {
	conn, ws, rec := live(t)
	for i := 0; i < 50; i++ {
		conn.Output("a", fmt.Sprint(i))
	}
	closed := make(chan struct{})
	took := make(chan time.Duration, 1)
	go func() {
		began := time.Now()
		conn.Close(messages.EndUser)
		took <- time.Since(began)
		conn.Output("a", "after the close")
		conn.Started("b")
		close(closed)
	}()
	for i := 0; i < 50; i++ {
		if m := read(t, ws); m.Text != fmt.Sprint(i) {
			t.Fatalf("message %d holds %q", i, m.Text)
		}
	}
	if got := readRaw(t, ws); got != `{"type":"session_end","reason":"user"}` {
		t.Fatalf("the controller got %s, want session_end", got)
	}
	// The close is a proper one, not a cut.
	ctx, cancel := context.WithTimeout(context.Background(), patience)
	defer cancel()
	if _, _, err := ws.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("the controller saw %v, want a normal close", err)
	}
	select {
	case <-closed:
	case <-time.After(patience):
		t.Fatal("the close did not come back")
	}
	if d := <-took; d > time.Second {
		t.Fatalf("the close took %v with a controller that answers", d)
	}
	rec.none(t)
}

// UNIT-CONNECTION-14. Requirement: SES-03.
func TestUNITCONNECTION14_TheControllerEndsTheSession(t *testing.T) {
	conn, ws, rec := live(t)
	ctx := context.Background()
	ws.Write(ctx, websocket.MessageText, []byte(`{"type":"session_end","reason":"shutdown"}`))
	ws.Write(ctx, websocket.MessageText, []byte(`{"type":"request","id":"r1","kind":"command","command":"ls"}`))
	rec.want(t, "session_end shutdown")
	wantClosed(t, ws)
	conn.Started("a")
	rec.none(t)
}

// UNIT-CONNECTION-15. Requirement: GATE-01.
func TestUNITCONNECTION15_ARequestOfAnUnknownKind(t *testing.T) {
	_, ws, rec := live(t)
	write(t, ws, `{"type":"request","id":"r1","kind":"read","path":"/etc/passwd","command":"ls"}`)
	write(t, ws, `{"type":"request","id":"r2","command":"ls"}`)
	write(t, ws, `{"type":"request","kind":"command","command":"ls"}`)
	for _, id := range []string{"r1", "r2"} {
		want := fmt.Sprintf(`{"type":"refused","id":%q,"reason":"unknown_kind"}`, id)
		if got := readRaw(t, ws); got != want {
			t.Fatalf("the controller got %s, want %s", got, want)
		}
	}
	rec.none(t)
	// Nothing was sent for the request with no id: the next message the
	// controller gets is the one sent now.
	write(t, ws, `{"type":"request","id":"r4","kind":"read"}`)
	if m := read(t, ws); m.ID != "r4" {
		t.Fatalf("the controller got an answer for %q, want r4", m.ID)
	}
}

// UNIT-CONNECTION-16. Requirement: GATE-07.
func TestUNITCONNECTION16_WhatIsNotAMessageIsIgnored(t *testing.T) {
	_, ws, rec := live(t)
	write(t, ws, `y`)
	write(t, ws, `{"type":"request","id":"r1","kind":"command","command":"ls"`)
	write(t, ws, `[{"type":"request","id":"r1","kind":"command","command":"ls"}]`)
	ws.Write(context.Background(), websocket.MessageBinary, []byte(`{"type":"request","id":"r1","kind":"command","command":"ls"}`))
	rec.none(t)
	write(t, ws, `{"type":"request","id":"r2","kind":"command","command":"ls"}`)
	rec.want(t, "request r2 ls")
}

// UNIT-CONNECTION-17. Requirement: LIM-10.
func TestUNITCONNECTION17_SendingNeverWaits(t *testing.T) {
	conn, _, rec := live(t)
	conn.mu.Lock()
	conn.maxQueued = 4 << 20
	conn.mu.Unlock()
	piece := strings.Repeat("x", 64<<10)
	began := time.Now()
	// The controller reads nothing. 64 MB is far more than the network
	// holds for it, so the line of waiting bytes must pass its limit.
	for i := 0; i < 1000; i++ {
		conn.Output("a", piece)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("a thousand sends took %v with a controller that reads nothing", took)
	}
	if got := rec.next(t); !strings.Contains(got, "does not take what is sent") {
		t.Fatalf("the event is %q, want lost with the reason", got)
	}
	rec.none(t)
}

// UNIT-CONNECTION-18. Requirement: SES-05.
func TestUNITCONNECTION18_AMessageThatIsTooLarge(t *testing.T) {
	_, ws, rec := live(t)
	data, _ := json.Marshal(messages.Message{Type: messages.TypeRequest, ID: "r1", Kind: messages.KindCommand, Command: strings.Repeat("x", readLimit+1)})
	ws.Write(context.Background(), websocket.MessageText, data)
	if got := rec.next(t); !strings.HasPrefix(got, "lost ") {
		t.Fatalf("the event is %q, want lost", got)
	}
	rec.none(t)
}

// UNIT-CONNECTION-19. Requirement: LIM-10.
func TestUNITCONNECTION19_CloseDoesNotWaitForASilentController(t *testing.T) {
	for _, flood := range []bool{false, true} {
		conn, ws, rec := live(t)
		conn.mu.Lock()
		conn.closeWait = 300 * time.Millisecond
		conn.maxQueued = 128 << 20
		conn.mu.Unlock()
		if flood {
			// More than the network holds, so the writer is stuck.
			piece := strings.Repeat("x", 64<<10)
			for i := 0; i < 500; i++ {
				conn.Output("a", piece)
			}
		}
		took := make(chan time.Duration, 1)
		go func() {
			began := time.Now()
			conn.Close(messages.EndUser)
			took <- time.Since(began)
		}()
		// A request that comes while the close is under way is not handed over.
		time.Sleep(50 * time.Millisecond)
		ws.Write(context.Background(), websocket.MessageText, []byte(`{"type":"request","id":"late","kind":"command","command":"ls"}`))
		if d := <-took; d > 2*time.Second {
			t.Fatalf("flood=%v: the close took %v with a controller that reads nothing", flood, d)
		}
		rec.none(t)
	}
}

// UNIT-CONNECTION-20. Requirement: KEY-10.
func TestUNITCONNECTION20_TheAPIAddressOfSessionOK(t *testing.T) {
	for _, given := range []string{"https://gangway.example.com", "", "not checked here\x1b[2J"} {
		c := newController(t)
		done := dialAsync(context.Background(), c, newRecorder())
		ws := c.accept(t)
		read(t, ws)
		answer, _ := json.Marshal(messages.Message{Type: messages.TypeSessionOK, APIAddress: given})
		write(t, ws, string(answer))
		d := await(t, done)
		if d.err != nil || d.address != given {
			t.Fatalf("the dial gave the address %q and %v, want %q", d.address, d.err, given)
		}
		d.conn.Close(messages.EndUser)
		ws.CloseNow()
	}
}

// countingRoad counts the calls that go through it.
type countingRoad struct{ calls atomic.Int32 }

func (r *countingRoad) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return http.DefaultTransport.RoundTrip(req)
}

// UNIT-CONNECTION-21. Requirement: HOST-08.
func TestUNITCONNECTION21_TheDialGoesOverTheRoadItIsGiven(t *testing.T) {
	c := newController(t)
	road := &countingRoad{}
	done := make(chan dialed, 1)
	go func() {
		conn, address, err := Dial(context.Background(), &http.Client{Transport: road}, c.srv.URL, startA, newRecorder())
		done <- dialed{conn, err, address}
	}()
	ws := c.accept(t)
	read(t, ws)
	write(t, ws, `{"type":"session_ok"}`)
	d := await(t, done)
	if d.err != nil {
		t.Fatal(d.err)
	}
	defer d.conn.Close(messages.EndUser)
	defer ws.CloseNow()
	if road.calls.Load() != 1 {
		t.Fatalf("%d calls went over the road that was given, want 1", road.calls.Load())
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-CONNECTION", "connection_test.go")
}
