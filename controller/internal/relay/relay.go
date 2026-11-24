// Package relay ties an open API call to its request on the host and
// passes messages both ways. One relay serves one session.
// The design is in docs/design.md.
package relay

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/kdop/gangway/shared/messages"
)

// The three kinds of answer an API call can end with.
const (
	StateRefused = "refused"
	StateEnded   = "ended"
	StateRunning = "running"
)

// Answer is what an API call ends with.
type Answer struct {
	State string

	Reason string // refused

	ID string // running

	How           string // ended
	ExitCode      *int
	Cut           bool
	BytesReplaced bool

	Output string // ended and running: what is new since the last answer
}

// Call is an open API call. It gets exactly one answer.
type Call chan Answer

type state int

const (
	open  state = iota // a call is open and waits
	asked              // the AI was told "still running", no call is open
	done               // the command ended while no call was open
)

type record struct {
	state   state
	started bool
	output  strings.Builder
	call    Call
	result  Answer
}

// Relay is safe for use from many goroutines.
type Relay struct {
	mu    sync.Mutex
	send  func(messages.Message)
	newID func() string
	reqs  map[string]*record
	ended bool
}

// New makes a relay. send hands a message to the host and must not block.
func New(send func(messages.Message)) *Relay {
	return &Relay{send: send, newID: randomID, reqs: map[string]*record{}}
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "r_" + hex.EncodeToString(b)
}

func answered(a Answer) Call {
	c := make(Call, 1)
	c <- a
	return c
}

// Ask sends a new request to the host and opens a call on it.
func (r *Relay) Ask(command string) (id string, call Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return "", answered(Answer{State: StateRefused, Reason: messages.ReasonSessionEnded})
	}
	id = r.newID()
	for r.reqs[id] != nil {
		id = r.newID()
	}
	rec := &record{state: open, call: make(Call, 1)}
	r.reqs[id] = rec
	r.send(messages.Message{Type: messages.TypeRequest, ID: id, Kind: messages.KindCommand, Command: command})
	return id, rec.call
}

// Wait is the AI's answer "wait more". ok is false when the id is not a
// command that waits for that answer.
func (r *Relay) Wait(id string) (call Call, ok bool) { return r.reopen(id, messages.TypeWait) }

// Stop is the AI's answer "stop". ok is as for Wait.
func (r *Relay) Stop(id string) (call Call, ok bool) { return r.reopen(id, messages.TypeStop) }

func (r *Relay) reopen(id, msgType string) (Call, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.reqs[id]
	if rec == nil {
		return nil, false
	}
	switch rec.state {
	case done:
		delete(r.reqs, id)
		return answered(rec.result), true
	case asked:
		rec.state = open
		rec.call = make(Call, 1)
		r.send(messages.Message{Type: msgType, ID: id})
		return rec.call, true
	}
	return nil, false
}

// HangUp tells the relay that the AI closed this call before it got an
// answer. The host is told to take the request back.
func (r *Relay) HangUp(id string, call Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.reqs[id]
	if rec == nil || rec.state != open || rec.call != call {
		return
	}
	delete(r.reqs, id)
	r.send(messages.Message{Type: messages.TypeCancel, ID: id})
}

// FromHost takes a message the host sent about a request. A message about
// a request the relay does not hold is dropped.
func (r *Relay) FromHost(msg messages.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.reqs[msg.ID]
	if rec == nil || rec.state == done {
		return
	}
	switch msg.Type {
	case messages.TypeStarted:
		rec.started = true
	case messages.TypeOutput:
		rec.output.WriteString(msg.Text)
	case messages.TypeStillRunning:
		if rec.state != open {
			return
		}
		rec.started = true
		rec.state = asked
		rec.call <- Answer{State: StateRunning, ID: msg.ID, Output: rec.take()}
		rec.call = nil
	case messages.TypeRefused:
		r.finish(msg.ID, rec, Answer{State: StateRefused, Reason: msg.Reason})
	case messages.TypeEnded:
		r.finish(msg.ID, rec, Answer{
			State: StateEnded, How: msg.How, ExitCode: msg.ExitCode,
			Cut: msg.Cut, BytesReplaced: msg.BytesReplaced, Output: rec.take(),
		})
	}
}

// take gives the output gathered since the last answer.
func (rec *record) take() string {
	s := rec.output.String()
	rec.output.Reset()
	return s
}

func (r *Relay) finish(id string, rec *record, a Answer) {
	if rec.state == open {
		rec.call <- a
		delete(r.reqs, id)
		return
	}
	rec.state = done
	rec.result = a
}

// End tells the relay that the session is over. Every open call is answered
// and nothing is held after it.
func (r *Relay) End() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended = true
	for id, rec := range r.reqs {
		if rec.state == open {
			rec.call <- Answer{State: StateRefused, Reason: messages.ReasonSessionEnded}
		}
		delete(r.reqs, id)
	}
}

// Counts tells how many requests wait for the user, and the id of the
// command that runs, if one does.
func (r *Relay) Counts() (waiting int, running string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, rec := range r.reqs {
		switch {
		case rec.state == done:
		case rec.started:
			running = id
		default:
			waiting++
		}
	}
	return waiting, running
}
