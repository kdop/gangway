// Package hostdoor takes the connections from the cli-tools. One
// connection carries one session. The design is in docs/design.md.
package hostdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/messages"
)

// Door is the handler for messages.HostPath.
type Door struct {
	Store *sessions.Store

	// APIAddress is the address the AI calls. Each host is told it in
	// session_ok, for its paste line.
	APIAddress string

	// StartWait is how long a new connection may take to send
	// session_start. OutQueue is how many messages may wait to be sent to
	// one host. Zero means the default.
	StartWait time.Duration
	OutQueue  int

	mu    sync.Mutex
	hosts map[*host]bool
}

const (
	defaultStartWait = 10 * time.Second
	defaultOutQueue  = 256
	writeWait        = 10 * time.Second
)

// host is one connected cli-tool.
type host struct {
	conn   *websocket.Conn
	out    chan messages.Message
	cancel context.CancelFunc
}

// send never blocks. A host that does not take its messages is cut off:
// its session ends, which is the safe way to fail.
func (h *host) send(msg messages.Message) {
	select {
	case h.out <- msg:
	default:
		h.cancel()
	}
}

func (d *Door) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(messages.MaxMessageBytes)

	startWait, queue := d.StartWait, d.OutQueue
	if startWait == 0 {
		startWait = defaultStartWait
	}
	if queue == 0 {
		queue = defaultOutQueue
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	h := &host{conn: conn, out: make(chan messages.Message, queue), cancel: cancel}

	startCtx, stop := context.WithTimeout(ctx, startWait)
	first, err := read(startCtx, conn)
	stop()
	if err != nil || first.Type != messages.TypeSessionStart || first.Limits == nil || first.LengthSeconds <= 0 {
		refuse(conn, "the first message must be session_start")
		return
	}
	sess := &sessions.Session{
		KeyHash: first.KeyHash,
		Host:    first.Host,
		Limits:  *first.Limits,
		Relay:   relay.New(h.send),
	}
	sess.Counts(d.Store.Now(), seconds(first.LengthSeconds))
	if err := d.Store.Add(sess); err != nil {
		refuse(conn, err.Error())
		return
	}
	d.track(h, true)
	defer func() {
		// The order matters: the key dies first, then the open calls are
		// answered, so no call can come in between and hang.
		d.Store.Remove(sess)
		sess.Relay.End()
		d.track(h, false)
		conn.CloseNow()
	}()

	go h.write(ctx)
	h.send(messages.Message{Type: messages.TypeSessionOK, APIAddress: d.APIAddress})

	for {
		msg, err := read(ctx, conn)
		if err != nil {
			return
		}
		switch msg.Type {
		case messages.TypeSessionEnd:
			return
		case messages.TypeRefused, messages.TypeStarted, messages.TypeOutput,
			messages.TypeStillRunning, messages.TypeEnded:
			sess.Relay.FromHost(msg)
		case messages.TypeTimeUp:
			sess.TimeIsUp()
		case messages.TypeExtended:
			sess.Extended(d.Store.Now(), seconds(msg.SecondsLeft))
		}
		// Any other type is dropped. No message from a host can do more
		// than answer its own requests, tell its own time or end its own
		// session.
	}
}

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }

func (h *host) write(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-h.out:
			b, _ := json.Marshal(msg)
			wctx, stop := context.WithTimeout(ctx, writeWait)
			err := h.conn.Write(wctx, websocket.MessageText, b)
			stop()
			if err != nil {
				h.cancel()
				return
			}
			if msg.Type == messages.TypeSessionEnd {
				h.cancel()
				return
			}
		}
	}
}

func read(ctx context.Context, conn *websocket.Conn) (messages.Message, error) {
	var msg messages.Message
	_, b, err := conn.Read(ctx)
	if err != nil {
		return msg, err
	}
	err = json.Unmarshal(b, &msg)
	return msg, err
}

func refuse(conn *websocket.Conn, why string) {
	if len(why) > 100 {
		why = why[:100]
	}
	conn.Close(websocket.StatusPolicyViolation, why)
}

func (d *Door) track(h *host, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hosts == nil {
		d.hosts = map[*host]bool{}
	}
	if on {
		d.hosts[h] = true
	} else {
		delete(d.hosts, h)
	}
}

// EndAll tells every host that its session is over, with this reason.
func (d *Door) EndAll(reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for h := range d.hosts {
		h.send(messages.Message{Type: messages.TypeSessionEnd, Reason: reason})
	}
}
