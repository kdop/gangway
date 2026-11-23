// Package loop ties the pieces of the cli-tool together. Every event is
// handed to it in turn, and it passes the event to the piece it belongs
// to. The design is in design.md.
package loop

import (
	"io"
	"time"

	"github.com/kdop/gangway/cli/internal/checks"
	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/line"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/cli/internal/settings"
	"github.com/kdop/gangway/cli/internal/terminal"
	"github.com/kdop/gangway/shared/messages"
)

// State is where the session stands.
type State int

const (
	Live   State = iota // requests are taken
	Ending              // the session has ended, and a command is still being stopped
	Over                // nothing is left to do
)

// Conn is the connection to the controller, as far as the loop needs it.
type Conn interface {
	gate.Sender
	runner.Sender
	session.Sender
	// Close tells the controller that the session ends, and why.
	Close(reason string)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Parts are the things the loop is handed from outside: the edges that a
// test replaces, and the session.
type Parts struct {
	Settings settings.Settings
	Session  *session.Session
	Conn     Conn
	Screen   io.Writer
	Clock    Clock
	Starter  runner.Starter
	Timers   runner.Timers

	// SessionTimers calls SessionTimer with the same number after a time.
	SessionTimers session.Timers
	// Length is the length this session starts with, when the user asked
	// for one at start. Zero: SESSION_LENGTH.
	Length time.Duration
}

// Loop holds the pieces. It is not safe for use from more than one
// goroutine: every method must be called from the same one, in turn.
type Loop struct {
	conn    Conn
	session *session.Session
	term    *terminal.Terminal
	line    *line.Line
	gate    *gate.Gate
	runner  *runner.Runner

	state  State
	reason string
	length time.Duration
}

// New puts the pieces together.
func New(p Parts) *Loop {
	l := &Loop{conn: p.Conn, session: p.Session, length: p.Length}
	if l.length <= 0 {
		l.length = p.Settings.SessionLength
	}
	l.term = terminal.New(p.Screen, p.Clock, p.Settings.DoubleCtrlCWindow, keys{l})
	l.line = line.New(p.Settings.MaxWaitingRequests)
	l.runner = runner.New(p.Starter, p.Timers, l.term, p.Conn, gateDone{l}, runner.Settings{
		Shell:        p.Settings.CommandShell,
		TimeLimit:    p.Settings.CommandTimeLimit,
		QuestionWait: p.Settings.QuestionWait,
		KillWait:     p.Settings.KillWait,
	})
	l.gate = gate.New(l.line, l.term, p.Conn, l.runner, p.Clock, p.Settings.PromptMinShow)
	l.session.StartClock(session.Edges{
		Clock:      p.Clock,
		Timers:     p.SessionTimers,
		Screen:     l.term,
		Sender:     p.Conn,
		Loop:       clockEvents{l},
		Length:     l.length,
		ExtendWait: p.Settings.ExtendWait,
		MinShow:    p.Settings.PromptMinShow,
	})
	return l
}

// clockEvents takes what the session's clock decides.
type clockEvents struct{ l *Loop }

// TimeUp refuses everything that waits. A command that runs is not killed:
// it gets no more time.
func (c clockEvents) TimeUp() {
	l := c.l
	for _, req := range l.line.Empty() {
		l.conn.Refused(req.ID, messages.ReasonTimeUp)
	}
	l.gate.TimeUp()
	l.runner.TimeUp()
	if l.runner.State() != runner.Idle {
		l.term.Note("The session's time is up. The command that runs gets no more time.")
	}
}

func (c clockEvents) NotExtended() {
	c.l.End(messages.EndTimeUp, "The session was not extended: the session ends.")
}

// gateDone lets the runner reach the gate, which is made after it.
type gateDone struct{ l *Loop }

func (g gateDone) Done() { g.l.gate.Done() }

// keys takes what the terminal makes of the keyboard.
type keys struct{ l *Loop }

func (k keys) Key(key []byte) {
	if k.l.state != Live {
		return
	}
	if k.l.session.State() == session.Extending {
		k.l.session.Press(key)
		return
	}
	k.l.gate.Key(key)
}

func (k keys) CtrlC() {
	l := k.l
	switch {
	case l.state != Live:
	case l.session.State() == session.Extending:
		l.session.CtrlC()
	case l.gate.State() == gate.Asking:
		l.gate.CtrlC()
	case l.gate.State() == gate.Busy:
		l.runner.CtrlC()
	default:
		l.End(messages.EndUser, "Ctrl+C: the session ends.")
	}
}

func (k keys) Quit() { k.l.End(messages.EndUser, "Ctrl+C twice: the session ends.") }

// State tells where the session stands.
func (l *Loop) State() State { return l.state }

// Begin shows the line to paste.
func (l *Loop) Begin() {
	l.term.Note("Connected. The session is live.")
	l.term.PasteLine(l.session.Address(), l.session.Key())
	l.term.Note("Each request is shown here. Press y to run it, n to refuse. Ctrl+C ends the session.")
	l.term.Note("The session lasts " + terminal.Length(l.length) + ". Then you are asked whether to extend it.")
}

// Input takes bytes read from the keyboard.
func (l *Loop) Input(chunk []byte) { l.term.Input(chunk) }

// Request takes a request from the controller. It has passed no check yet.
func (l *Loop) Request(req request.Request) {
	if l.state != Live {
		return
	}
	if !l.session.Takes() {
		l.conn.Refused(req.ID, messages.ReasonTimeUp)
		l.term.Refused(messages.ReasonTimeUp)
		l.settle()
		return
	}
	if reason, ok := checks.Check(req); !ok {
		l.conn.Refused(req.ID, reason)
		l.term.Refused(reason)
		return
	}
	if !l.line.Add(req, l.gate.State() == gate.Asking) {
		l.conn.Refused(req.ID, messages.ReasonLineFull)
		l.term.Refused(messages.ReasonLineFull)
		return
	}
	l.gate.Ready()
}

// Wait is the AI's answer "wait more" for the request with this ID.
func (l *Loop) Wait(id string) {
	if id != "" && id == l.runner.RequestID() {
		l.runner.Wait()
	}
}

// Stop is the AI's answer "stop" for the request with this ID.
func (l *Loop) Stop(id string) {
	if id != "" && id == l.runner.RequestID() {
		l.runner.Stop()
	}
}

// Cancel tells that the AI hung up on the request with this ID.
func (l *Loop) Cancel(id string) {
	if l.state != Live || id == "" {
		return
	}
	if l.line.Remove(id) {
		l.conn.Refused(id, messages.ReasonAIGone)
		return
	}
	l.gate.Cancel(id)
	l.runner.Cancel(id)
}

// Output is a piece of what the running command wrote.
func (l *Loop) Output(piece []byte) { l.runner.Output(piece) }

// Exited tells that the shell of the running command ended.
func (l *Loop) Exited(exitCode int) {
	l.runner.Exited(exitCode)
	l.settle()
	l.finish()
}

// SessionTimer tells that the session timer with this number ran out.
func (l *Loop) SessionTimer(number int) {
	if l.state != Live {
		return
	}
	l.session.Timer(number)
	l.settle()
}

// settle tells the session's clock when no command runs. The question to
// extend is shown only then.
func (l *Loop) settle() {
	if l.state == Live && l.runner.State() == runner.Idle {
		l.session.Idle()
	}
}

// Timer tells that the timer with this number ran out.
func (l *Loop) Timer(number int) { l.runner.Timer(number) }

// SessionEnd is the controller ending the session.
func (l *Loop) SessionEnd(reason string) {
	l.End(reason, "The controller ended the session.")
}

// Lost tells that the connection to the controller is gone.
func (l *Loop) Lost() {
	l.End(messages.EndConnectionLost, "The connection to the controller was lost: the session ends.")
}

// End ends the session. Nothing new runs from here on. A command that runs
// is stopped, and the loop is Over once it is gone.
func (l *Loop) End(reason, note string) {
	if l.state != Live {
		return
	}
	l.state = Ending
	l.reason = reason
	l.session.End()
	l.gate.End()
	l.term.Forget()
	for _, req := range l.line.Empty() {
		l.conn.Refused(req.ID, messages.ReasonSessionEnded)
	}
	l.term.Note(note)
	l.runner.End()
	l.finish()
}

func (l *Loop) finish() {
	if l.state != Ending || l.runner.State() != runner.Idle {
		return
	}
	l.state = Over
	l.conn.Close(l.reason)
	l.term.Note("The session is over. Its key is dead.")
}
