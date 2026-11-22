// Package connection holds the websocket to the controller. It sends the
// cli-tool's answers as messages and hands the controller's messages over
// as events. The design is in design.md.
package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/shared/messages"
)

const (
	// dialWait is how long Dial waits for the controller when the caller
	// set no time limit of its own.
	dialWait = 10 * time.Second

	// closeWait is how long Close waits for the last messages to leave.
	closeWait = 2 * time.Second

	// readLimit is the largest message taken from the controller.
	readLimit = 1 << 20

	// defaultMaxQueued is how many bytes may wait to be sent. A controller
	// that falls further behind than this counts as lost.
	defaultMaxQueued = 8 << 20
)

// Handler takes what comes from the controller. The connection calls it
// from a goroutine of its own, one call at a time, in the order the
// messages came. An implementation must hand each call over to the one
// loop of the cli-tool and come back at once.
type Handler interface {
	// Request is a request from an AI. It has passed no check yet.
	Request(req request.Request)
	// Wait is the AI's answer "wait more" for the request with this ID.
	Wait(id string)
	// Stop is the AI's answer "stop" for the request with this ID.
	Stop(id string)
	// Cancel says the AI hung up on the request with this ID.
	Cancel(id string)
	// SessionEnd says the controller ends the session. No call comes
	// after it.
	SessionEnd(reason string)
	// Lost says the connection is gone and was not ended by either side
	// on purpose. No call comes after it.
	Lost(err error)
}

// Start is what the cli-tool tells the controller when it opens a session.
// It holds the hash of the key, never the key.
type Start struct {
	KeyHash string
	Host    string
	Limits  messages.Limits
}

// Conn is a live connection. Its send methods may be called from any
// goroutine. They never wait for the network.
type Conn struct {
	ws      *websocket.Conn
	handler Handler

	mu        sync.Mutex
	cond      *sync.Cond
	queue     [][]byte
	queued    int
	maxQueued int
	closeWait time.Duration
	closed    bool  // nothing more is taken for sending
	quiet     bool  // the end was asked for, so it is not a loss
	polite    bool  // close the websocket the proper way once the queue is empty
	closing   bool  // the proper close of the websocket has begun
	cause     error // why the connection was cut from this side

	writerDone chan struct{}
}

// Dial opens the websocket, sends session_start and waits for session_ok.
// client and controller come from the network piece: what opens a
// connection on its road, and the controller's address on that road.
// apiAddress is the address the controller gave for the paste line. It is
// not checked here. No session exists unless Dial comes back without an
// error.
func Dial(ctx context.Context, client *http.Client, controller string, start Start, handler Handler) (conn *Conn, apiAddress string, err error) {
	c, address, err := dial(ctx, client, controller, start, handler)
	if err != nil {
		return nil, "", err
	}
	return c, address, nil
}

func dial(ctx context.Context, client *http.Client, controller string, start Start, handler Handler) (*Conn, string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dialWait)
		defer cancel()
	}
	ws, _, err := websocket.Dial(ctx, strings.TrimRight(controller, "/")+messages.HostPath, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return nil, "", fmt.Errorf("cannot reach the controller: %w", err)
	}
	ws.SetReadLimit(readLimit)

	limits := start.Limits
	hello, _ := json.Marshal(messages.Message{
		Type:    messages.TypeSessionStart,
		KeyHash: start.KeyHash,
		Host:    start.Host,
		Limits:  &limits,
	})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		ws.CloseNow()
		return nil, "", fmt.Errorf("cannot start the session: %w", err)
	}
	kind, data, err := ws.Read(ctx)
	if err != nil {
		ws.CloseNow()
		return nil, "", fmt.Errorf("the controller did not take the session: %w", err)
	}
	var answer messages.Message
	if kind != websocket.MessageText || json.Unmarshal(data, &answer) != nil || answer.Type != messages.TypeSessionOK {
		ws.CloseNow()
		return nil, "", errors.New("the controller did not take the session: its first message was not session_ok")
	}

	c := &Conn{ws: ws, handler: handler, maxQueued: defaultMaxQueued, closeWait: closeWait, writerDone: make(chan struct{})}
	c.cond = sync.NewCond(&c.mu)
	go c.write()
	go c.read()
	return c, answer.APIAddress, nil
}

// Started says the user pressed y for the request.
func (c *Conn) Started(id string) {
	c.send(messages.Message{Type: messages.TypeStarted, ID: id})
}

// Refused says the request will not run.
func (c *Conn) Refused(id, reason string) {
	c.send(messages.Message{Type: messages.TypeRefused, ID: id, Reason: reason})
}

// Output is a piece of what the command wrote.
func (c *Conn) Output(id, text string) {
	c.send(messages.Message{Type: messages.TypeOutput, ID: id, Text: text})
}

// StillRunning says the time limit passed and the AI must answer.
func (c *Conn) StillRunning(id string) {
	c.send(messages.Message{Type: messages.TypeStillRunning, ID: id})
}

// Ended says how the command ended.
func (c *Conn) Ended(id string, result runner.Result) {
	m := messages.Message{Type: messages.TypeEnded, ID: id, How: result.How, BytesReplaced: result.BytesReplaced}
	if result.How == messages.HowExit {
		code := result.ExitCode
		m.ExitCode = &code
	}
	c.send(m)
}

// Close tells the controller that the session ends, waits a short time for
// what is still to be sent, and closes the connection. The handler gets no
// Lost for it. An event that was already on its way may still arrive.
func (c *Conn) Close(reason string) {
	end, _ := json.Marshal(messages.Message{Type: messages.TypeSessionEnd, Reason: reason})
	c.mu.Lock()
	if !c.closed {
		c.queue = append(c.queue, end)
		c.closed = true
		c.polite = true
	}
	c.quiet = true
	wait := c.closeWait
	c.cond.Broadcast()
	c.mu.Unlock()

	select {
	case <-c.writerDone:
	case <-time.After(wait):
		c.mu.Lock()
		closing := c.closing
		c.mu.Unlock()
		if closing {
			// Everything was sent. The proper close ends by itself, and
			// cutting in now would only wait for it.
			return
		}
	}
	c.ws.CloseNow()
}

func (c *Conn) send(m messages.Message) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if c.queued+len(data) > c.maxQueued {
		c.cause = errors.New("the controller does not take what is sent")
		c.cutLocked()
		c.mu.Unlock()
		c.ws.CloseNow()
		return
	}
	c.queue = append(c.queue, data)
	c.queued += len(data)
	c.cond.Signal()
	c.mu.Unlock()
}

// cutLocked stops all sending and drops what waits. The caller holds mu.
func (c *Conn) cutLocked() {
	c.closed = true
	c.polite = false
	c.queue = nil
	c.queued = 0
	c.cond.Broadcast()
}

func (c *Conn) write() {
	defer close(c.writerDone)
	for {
		c.mu.Lock()
		for len(c.queue) == 0 && !c.closed {
			c.cond.Wait()
		}
		if len(c.queue) == 0 {
			polite := c.polite
			c.closing = polite
			c.mu.Unlock()
			if polite {
				// This waits for the controller to answer the close, a few
				// seconds at most, and then closes in any case.
				c.ws.Close(websocket.StatusNormalClosure, "")
			}
			return
		}
		data := c.queue[0]
		c.queue = c.queue[1:]
		c.queued -= len(data)
		c.mu.Unlock()

		if err := c.ws.Write(context.Background(), websocket.MessageText, data); err != nil {
			c.mu.Lock()
			if c.cause == nil {
				c.cause = err
			}
			c.cutLocked()
			c.mu.Unlock()
			c.ws.CloseNow()
			return
		}
	}
}

func (c *Conn) read() {
	for {
		kind, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.mu.Lock()
			quiet := c.quiet
			if c.cause != nil {
				err = c.cause
			}
			c.quiet = true
			c.cutLocked()
			c.mu.Unlock()
			c.ws.CloseNow()
			if !quiet {
				c.handler.Lost(err)
			}
			return
		}
		if kind != websocket.MessageText {
			continue
		}
		var m messages.Message
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if c.isQuiet() {
			continue
		}
		// Only the types named here do anything. No type approves a
		// request, extends a session or changes a setting.
		switch m.Type {
		case messages.TypeRequest:
			if m.ID == "" {
				continue
			}
			if m.Kind != messages.KindCommand {
				c.Refused(m.ID, messages.ReasonUnknownKind)
				continue
			}
			c.handler.Request(request.Request{ID: m.ID, Command: m.Command})
		case messages.TypeWait:
			if m.ID != "" {
				c.handler.Wait(m.ID)
			}
		case messages.TypeStop:
			if m.ID != "" {
				c.handler.Stop(m.ID)
			}
		case messages.TypeCancel:
			if m.ID != "" {
				c.handler.Cancel(m.ID)
			}
		case messages.TypeSessionEnd:
			c.mu.Lock()
			c.quiet = true
			c.cutLocked()
			c.mu.Unlock()
			c.handler.SessionEnd(m.Reason)
			c.ws.CloseNow()
			return
		}
	}
}

func (c *Conn) isQuiet() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quiet
}
