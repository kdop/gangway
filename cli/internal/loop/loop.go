// Package loop ties the pieces of the cli-tool together. Every event is
// handed to it in turn, and it passes the event to the piece it belongs
// to. The design is in docs/design.md.
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

// Log is the log of the session, as far as the loop needs it.
type Log interface {
	Session(text string)
	Request(req request.Request)
	Started(id string)
	Refused(id, reason string)
	Output(id, text string)
	Ended(id string, result runner.Result)
	// Err gives the first failure to write.
	Err() error
	// File is the path of the log file, and Old the number of old log
	// files beside it.
	File() string
	Old() int
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
	// Admin holds the bounds of the admin file. A length above its
	// maximum is cut here, and the screen says so.
	Admin settings.Admin

	// Log is the log of the session. Nil: nothing is logged.
	Log Log

	// Host is the name of this host, and Network how the controller is
	// reached. Both are for the screen only.
	Host    string
	Network string
	// Look is how the screen is dressed.
	Look terminal.Look
}

// Loop holds the pieces. It is not safe for use from more than one
// goroutine: every method must be called from the same one, in turn.
type Loop struct {
	conn    Conn
	log     Log
	session *session.Session
	term    *terminal.Terminal
	line    *line.Line
	gate    *gate.Gate
	runner  *runner.Runner

	state  State
	reason string
	length time.Duration
	max    time.Duration // SESSION_MAX. Zero: no maximum
	asked  time.Duration // the length before the maximum cut it. Zero: it was not cut
	admin  string        // the admin file, for the screen

	// For the screen only.
	clock   Clock
	host    string
	network string
	began   time.Time
	why     string // why the session ended
	counts  *counts
}

// counts holds how many requests ran and how many were refused, for the
// end of the session.
type counts struct{ ran, refused int }

// New puts the pieces together.
func New(p Parts) *Loop {
	l := &Loop{conn: p.Conn, log: p.Log, session: p.Session, length: p.Length,
		clock: p.Clock, host: p.Host, network: p.Network, counts: &counts{}}
	if l.log == nil {
		l.log = noLog{}
	}
	// Every answer passes the log on its way to the controller.
	l.conn = logged{p.Conn, l.log, l.counts}
	if l.length <= 0 {
		l.length = p.Settings.SessionLength
	}
	l.max, l.admin = p.Admin.SessionMax, p.Admin.File
	if length, cut := p.Admin.Length(l.length); cut {
		l.asked, l.length = l.length, length
	}
	l.term = terminal.New(p.Screen, p.Clock, p.Settings.DoubleCtrlCWindow, keys{l})
	l.term.Dress(p.Look)
	l.term.TimeLeft(l.session.Left)
	l.line = line.New(p.Settings.MaxWaitingRequests)
	l.runner = runner.New(p.Starter, p.Timers, l.term, l.conn, gateDone{l}, runner.Settings{
		Shell:        p.Settings.CommandShell,
		TimeLimit:    p.Settings.CommandTimeLimit,
		QuestionWait: p.Settings.QuestionWait,
		KillWait:     p.Settings.KillWait,
	})
	l.gate = gate.New(l.line, l.term, l.conn, l.runner, p.Clock, p.Settings.PromptMinShow)
	l.session.StartClock(session.Edges{
		Clock:      p.Clock,
		Timers:     p.SessionTimers,
		Screen:     l.term,
		Sender:     l.conn,
		Loop:       clockEvents{l},
		Length:     l.length,
		Max:        l.max,
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
	c.l.End(messages.EndTimeUp, "The session was not extended: the session ends.", "the time was up and the session was not extended")
}

func (c clockEvents) MaxReached() {
	c.l.End(messages.EndTimeUp, "The session has lasted "+terminal.Length(c.l.max)+", the maximum: the session ends.", "it reached the maximum of "+terminal.Short(c.l.max))
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
		l.End(messages.EndUser, "Ctrl+C: the session ends.", "you pressed Ctrl+C")
	}
}

func (k keys) Quit() {
	k.l.End(messages.EndUser, "Ctrl+C twice: the session ends.", "you pressed Ctrl+C twice")
}

// State tells where the session stands.
func (l *Loop) State() State { return l.state }

// Begin shows the session and the line to paste.
func (l *Loop) Begin() {
	l.began = l.clock.Now()
	l.term.Start(terminal.Start{
		Host:    l.host,
		Network: l.network,
		Length:  l.length,
		Max:     l.max,
		Asked:   l.asked,
		Admin:   l.admin,
		Log:     l.log.File(),
		Address: l.session.Address(),
		Key:     l.session.Key(),
	})
	l.log.Session("started: it lasts " + terminal.Length(l.length))
	l.check()
}

// Input takes bytes read from the keyboard.
func (l *Loop) Input(chunk []byte) {
	l.term.Input(chunk)
	l.check()
}

// Request takes a request from the controller. It has passed no check yet.
func (l *Loop) Request(req request.Request) {
	if l.state != Live {
		return
	}
	// A request that cannot be logged goes no further.
	l.log.Request(req)
	if l.log.Err() != nil {
		l.conn.Refused(req.ID, messages.ReasonSessionEnded)
		l.check()
		return
	}
	defer l.check()
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
	defer l.check()
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
	l.check()
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
	l.check()
}

// settle tells the session's clock when no command runs. The question to
// extend is shown only then.
func (l *Loop) settle() {
	if l.state == Live && l.runner.State() == runner.Idle {
		l.session.Idle()
	}
}

// Timer tells that the timer with this number ran out.
func (l *Loop) Timer(number int) {
	l.runner.Timer(number)
	l.check()
}

// SessionEnd is the controller ending the session.
func (l *Loop) SessionEnd(reason string) {
	l.End(reason, "The controller ended the session.", "the controller ended it")
}

// Lost tells that the connection to the controller is gone.
func (l *Loop) Lost() {
	l.End(messages.EndConnectionLost, "The connection to the controller was lost: the session ends.", "the connection to the controller was lost")
}

// End ends the session. Nothing new runs from here on. A command that runs
// is stopped, and the loop is Over once it is gone. note goes to the log,
// and why to the screen.
func (l *Loop) End(reason, note, why string) {
	if l.state != Live {
		return
	}
	l.state = Ending
	l.reason = reason
	l.why = why
	l.log.Session("ended: " + reason + ". " + note)
	l.session.End()
	l.gate.End()
	l.term.Forget()
	for _, req := range l.line.Empty() {
		l.conn.Refused(req.ID, messages.ReasonSessionEnded)
	}
	if l.runner.State() != runner.Idle {
		l.term.Note("The session ends: " + why + ". The command is being stopped.")
	}
	l.runner.End()
	l.finish()
}

func (l *Loop) finish() {
	if l.state != Ending || l.runner.State() != runner.Idle {
		return
	}
	l.state = Over
	l.conn.Close(l.reason)
	var lasted time.Duration
	if !l.began.IsZero() {
		lasted = l.clock.Now().Sub(l.began)
	}
	l.term.Over(terminal.Over{
		Why:     l.why,
		Lasted:  lasted,
		Ran:     l.counts.ran,
		Refused: l.counts.refused,
		Log:     l.log.File(),
		LogOld:  l.log.Old() > 0,
		LogBad:  l.log.Err() != nil,
	})
}

// check ends the session when the log could not be written. It tells
// whether it did. It is called between events only, never from inside a
// piece.
func (l *Loop) check() bool {
	if l.state != Live || l.log.Err() == nil {
		return false
	}
	l.End(messages.EndUser, "The log cannot be written: the session ends.", "the log could not be written")
	return true
}

// logged is the connection with the log in front: what is sent to the
// controller about a request is written down first, and counted.
type logged struct {
	Conn
	log    Log
	counts *counts
}

func (c logged) Started(id string) {
	c.counts.ran++
	c.log.Started(id)
	c.Conn.Started(id)
}

func (c logged) Refused(id, reason string) {
	c.counts.refused++
	c.log.Refused(id, reason)
	c.Conn.Refused(id, reason)
}

func (c logged) Output(id, text string) {
	c.log.Output(id, text)
	c.Conn.Output(id, text)
}

func (c logged) Ended(id string, result runner.Result) {
	c.log.Ended(id, result)
	c.Conn.Ended(id, result)
}

func (c logged) TimeUp() {
	c.log.Session("time is up")
	c.Conn.TimeUp()
}

func (c logged) Extended(secondsLeft int) {
	c.log.Session("extended: " + terminal.Length(time.Duration(secondsLeft)*time.Second) + " left")
	c.Conn.Extended(secondsLeft)
}

// noLog logs nothing.
type noLog struct{}

func (noLog) Session(string)              {}
func (noLog) Request(request.Request)     {}
func (noLog) Started(string)              {}
func (noLog) Refused(string, string)      {}
func (noLog) Output(string, string)       {}
func (noLog) Ended(string, runner.Result) {}
func (noLog) Err() error                  { return nil }
func (noLog) File() string                { return "" }
func (noLog) Old() int                    { return 0 }
