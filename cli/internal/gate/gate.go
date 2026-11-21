// Package gate shows one request at a time and takes the y or n for it.
// It is the only way to the runner. The design is in design.md.
package gate

import (
	"time"

	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/shared/messages"
)

// State is where the gate stands.
type State int

const (
	Free   State = iota // nothing is in front
	Asking              // a prompt is on the screen and waits for a key
	Busy                // an approved request is with the runner
	Ended               // the session is over
)

func (s State) String() string {
	switch s {
	case Free:
		return "Free"
	case Asking:
		return "Asking"
	case Busy:
		return "Busy"
	case Ended:
		return "Ended"
	}
	return "unknown"
}

// Answer is how a prompt was closed, for the screen.
type Answer int

const (
	Yes    Answer = iota // the user pressed y
	No                   // the user pressed n or Ctrl+C
	AIGone               // the AI hung up
)

// The reasons sent with a refusal. They are part of the contract in
// architecture.md.
const (
	ReasonUser         = messages.ReasonUser
	ReasonAIGone       = messages.ReasonAIGone
	ReasonSessionEnded = messages.ReasonSessionEnded
)

// Line holds the requests that wait.
type Line interface {
	// Next removes and returns the oldest waiting request.
	Next() (request.Request, bool)
}

// Screen is the terminal, as far as the gate needs it.
type Screen interface {
	Prompt(request.Request)
	Answer(request.Request, Answer)
}

// Sender sends the gate's answers to the controller.
type Sender interface {
	Started(id string)
	Refused(id, reason string)
}

// Runner runs an approved request.
type Runner interface {
	Run(*Approval)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Approval is the user's yes for one request. Its fields are hidden, so
// only this package can make one that works.
type Approval struct {
	req  request.Request
	live bool
}

// Use hands out the approved request. It works once. A used approval, a nil
// one and one made outside this package all give false.
func (a *Approval) Use() (request.Request, bool) {
	if a == nil || !a.live {
		return request.Request{}, false
	}
	a.live = false
	return a.req, true
}

// Gate is the state machine. It is not safe for use from more than one
// goroutine: the one loop of the cli-tool owns it.
type Gate struct {
	line    Line
	screen  Screen
	sender  Sender
	runner  Runner
	clock   Clock
	minShow time.Duration

	state   State
	front   request.Request
	shownAt time.Time
}

// New makes a gate in the Free state. minShow is PROMPT_MIN_SHOW.
func New(line Line, screen Screen, sender Sender, runner Runner, clock Clock, minShow time.Duration) *Gate {
	return &Gate{line: line, screen: screen, sender: sender, runner: runner, clock: clock, minShow: minShow}
}

// State tells where the gate stands.
func (g *Gate) State() State { return g.state }

// Ready tells the gate that a request waits in the line.
func (g *Gate) Ready() {
	if g.state == Free {
		g.takeNext()
	}
}

// Key is one key press, as the bytes the key sent.
func (g *Gate) Key(key []byte) {
	if g.state != Asking {
		return
	}
	if g.clock.Now().Sub(g.shownAt) < g.minShow {
		return
	}
	if len(key) != 1 {
		return
	}
	switch key[0] {
	case 'y':
		g.approve()
	case 'n':
		g.refuse(No, ReasonUser)
	}
}

// CtrlC is one Ctrl+C. It is not held back by minShow: it can only refuse.
func (g *Gate) CtrlC() {
	if g.state == Asking {
		g.refuse(No, ReasonUser)
	}
}

// Cancel tells the gate that the AI hung up on the request with this ID.
func (g *Gate) Cancel(id string) {
	if g.state == Asking && g.front.ID == id {
		g.refuse(AIGone, ReasonAIGone)
	}
}

// Done tells the gate that the runner is finished with the request it got.
func (g *Gate) Done() {
	if g.state == Busy {
		g.free()
	}
}

// End tells the gate that the session is over.
func (g *Gate) End() {
	if g.state == Asking {
		g.sender.Refused(g.front.ID, ReasonSessionEnded)
	}
	g.front = request.Request{}
	g.state = Ended
}

// approve is the only place an approval is made.
func (g *Gate) approve() {
	req := g.front
	g.screen.Answer(req, Yes)
	g.sender.Started(req.ID)
	g.state = Busy
	g.runner.Run(&Approval{req: req, live: true})
}

func (g *Gate) refuse(answer Answer, reason string) {
	req := g.front
	g.screen.Answer(req, answer)
	g.sender.Refused(req.ID, reason)
	g.free()
}

func (g *Gate) free() {
	g.front = request.Request{}
	g.state = Free
	g.takeNext()
}

func (g *Gate) takeNext() {
	req, ok := g.line.Next()
	if !ok {
		return
	}
	g.front = req
	g.screen.Prompt(req)
	g.shownAt = g.clock.Now()
	g.state = Asking
}
