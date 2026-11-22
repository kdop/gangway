package acceptance

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kdop/gangway/shared/messages"
)

// standIn is a controller played by the test, with one real cli-tool
// connected to it. It can send what a real controller never would.
type standIn struct {
	t    *testing.T
	conn *websocket.Conn
	*world
}

// newStandIn starts a real cli-tool against a controller played by the
// test.
func newStandIn(t *testing.T) *standIn {
	t.Helper()
	beside(t)
	s := &standIn{t: t}
	connected := make(chan *websocket.Conn, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(messages.HostPath, func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var first messages.Message
		_, b, err := conn.Read(r.Context())
		if err != nil || json.Unmarshal(b, &first) != nil || first.Type != messages.TypeSessionStart {
			conn.CloseNow()
			return
		}
		ok, _ := json.Marshal(messages.Message{Type: messages.TypeSessionOK, APIAddress: "http://" + r.Host})
		conn.Write(r.Context(), websocket.MessageText, ok)
		connected <- conn
		<-r.Context().Done()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	t.Cleanup(func() { server.Close() })

	s.world = &world{t: t, home: t.TempDir(), listen: ln.Addr().String(), address: "http://" + ln.Addr().String()}
	s.startCli(nil)
	select {
	case s.conn = <-connected:
	case <-time.After(soon):
		t.Fatal("the cli-tool did not connect")
	}
	return s
}

// send sends one message to the cli-tool, as written.
func (s *standIn) send(raw string) {
	s.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), soon)
	defer stop()
	if err := s.conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		s.t.Fatal(err)
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestGATE07_1_AMessageFromTheControllerIsNoYes(t *testing.T) {
	s := newStandIn(t)
	command := quote("touch " + s.file("ran"))
	s.send(`{"type":"request","id":"r_1","kind":"command","command":` + command + `}`)
	s.prompt(1)
	for _, raw := range []string{
		`{"type":"approved","id":"r_1"}`,
		`{"type":"approve","id":"r_1","answer":"y"}`,
		`{"type":"answer","id":"r_1","answer":"y"}`,
		`{"type":"started","id":"r_1"}`,
		`{"type":"request","id":"r_1","approved":true}`,
		`{"type":"wait","id":"r_1"}`,
	} {
		s.send(raw)
	}
	s.notRun("ran")
	s.promptWaits(1)
	if strings.Contains(s.screen.text(), question+"y") {
		t.Fatal("the screen shows a yes")
	}
}

func TestGATE07_3_ARequestMarkedAsApprovedIsAskedAllTheSame(t *testing.T) {
	s := newStandIn(t)
	command := quote("touch " + s.file("ran"))
	s.send(`{"type":"request","id":"r_1","kind":"command","command":` + command + `,"approved":true,"answer":"y","state":"approved"}`)
	s.prompt(1)
	s.shows("AI asks to run:\n  > touch " + s.file("ran") + "\n" + question)
	s.notRun("ran")
	s.promptWaits(1)
}

// fakeHost connects to the real controller as a cli-tool would, and asks
// for a session with this key hash. It tells whether the session was taken.
func (w *world) fakeHost(keyHash string) bool {
	w.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), soon)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws://"+w.listen+messages.HostPath, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { conn.CloseNow() })
	limits := messages.Limits{CommandSeconds: 60, AnswerSeconds: 60, MaxWaiting: 20}
	first, _ := json.Marshal(messages.Message{Type: messages.TypeSessionStart, KeyHash: keyHash, Host: "fake", Limits: &limits})
	if err := conn.Write(ctx, websocket.MessageText, first); err != nil {
		w.t.Fatal(err)
	}
	var answer messages.Message
	_, b, err := conn.Read(ctx)
	return err == nil && json.Unmarshal(b, &answer) == nil && answer.Type == messages.TypeSessionOK
}

func TestKEY09_1_AKeyHashOfALiveSessionIsRefused(t *testing.T) {
	w := newWorld(t)
	if w.fakeHost(messages.KeyHash(w.key)) {
		t.Fatal("the controller took a second session with the same key hash")
	}
	a := w.ask("echo first")
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "first\n" {
		t.Fatalf("the request did not reach the first cli-tool: %s", got.Body)
	}
}

func TestKEY09_2_TheHashOfAnEndedSessionIsTakenAsNew(t *testing.T) {
	w := newWorld(t)
	w.endByCtrlC()
	w.keyRefused(w.key)
	if !w.fakeHost(messages.KeyHash(w.key)) {
		t.Fatal("the controller refused the hash of an ended session")
	}
	w.live()
}
