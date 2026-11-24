// Package runner runs one approved command, sends what it writes, asks
// "wait or stop" at the time limit, and kills it when told.
// The design is in docs/design.md.
package runner

import (
	"time"

	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/shared/messages"
)

// State is where the runner stands.
type State int

const (
	Idle     State = iota // nothing runs
	Running               // a command runs and its time limit counts
	Asked                 // the AI was asked to wait or stop
	Stopping              // the command was told to stop and has not ended yet
)

func (s State) String() string {
	switch s {
	case Idle:
		return "Idle"
	case Running:
		return "Running"
	case Asked:
		return "Asked"
	case Stopping:
		return "Stopping"
	}
	return "unknown"
}

// The ways a command ends. They are part of the contract in
// docs/architecture.md.
const (
	HowExit               = messages.HowExit
	HowKilledByUser       = messages.HowKilledByUser
	HowKilledByAI         = messages.HowKilledByAI
	HowKilledNoAnswer     = messages.HowKilledNoAnswer
	HowKilledTimeUp       = messages.HowKilledTimeUp
	HowKilledAIGone       = messages.HowKilledAIGone
	HowKilledSessionEnded = messages.HowKilledSessionEnded
	HowStartFailed        = messages.HowStartFailed
)

// Result is how a command ended. ExitCode is set only when How is HowExit.
type Result struct {
	How           string
	ExitCode      int
	BytesReplaced bool
}

// Process is a started command.
type Process interface {
	// Stop asks the command and everything it started to stop.
	Stop()
	// Kill kills by force what is left of the command.
	Kill()
}

// Starter starts a command through a shell. The command's output and its
// end come back to the runner as the Output and Exited events.
type Starter interface {
	Start(shell, command string) (Process, error)
}

// Timers calls the runner's Timer event with the same number after d.
type Timers interface {
	Start(d time.Duration, number int)
}

// Screen is the terminal, as far as the runner needs it.
type Screen interface {
	Output(text string)
	Ended(request.Request, Result)
}

// Sender sends the runner's messages to the controller.
type Sender interface {
	Output(id, text string)
	StillRunning(id string)
	Ended(id string, result Result)
}

// Gate is told when the runner is free again.
type Gate interface {
	Done()
}

// Settings are the values the runner takes from the settings.
type Settings struct {
	Shell        string        // COMMAND_SHELL
	TimeLimit    time.Duration // COMMAND_TIME_LIMIT
	QuestionWait time.Duration // QUESTION_WAIT
	KillWait     time.Duration // KILL_WAIT
}

// Runner is the state machine. It is not safe for use from more than one
// goroutine: the one loop of the cli-tool owns it, and the starter and the
// timers must hand their events to that loop.
type Runner struct {
	starter  Starter
	timers   Timers
	screen   Screen
	sender   Sender
	gate     Gate
	settings Settings

	state State
	req   request.Request
	proc  Process
	how   string
	text  text
	timer int

	// timeUp says the session's time ran out while this command ran: it
	// gets no more time.
	timeUp bool
}

// New makes a runner in the Idle state.
func New(starter Starter, timers Timers, screen Screen, sender Sender, g Gate, settings Settings) *Runner {
	return &Runner{starter: starter, timers: timers, screen: screen, sender: sender, gate: g, settings: settings}
}

// State tells where the runner stands.
func (r *Runner) State() State { return r.state }

// Run starts the request of an approval. It is the only way in.
func (r *Runner) Run(a *gate.Approval) {
	if r.state != Idle {
		return
	}
	req, ok := a.Use()
	if !ok {
		return
	}
	r.req = req
	r.text = text{}
	proc, err := r.starter.Start(r.settings.Shell, req.Command)
	if err != nil {
		r.output("gangway: the command could not be started: " + err.Error() + "\n")
		r.finish(Result{How: HowStartFailed})
		return
	}
	r.proc = proc
	r.state = Running
	r.startTimer(r.settings.TimeLimit)
}

// Output is a piece of what the command wrote.
func (r *Runner) Output(piece []byte) {
	if r.state == Idle {
		return
	}
	r.output(r.text.add(piece))
}

// Exited tells the runner that the shell ended.
func (r *Runner) Exited(exitCode int) {
	if r.state == Idle {
		return
	}
	r.output(r.text.flush())
	if r.state == Stopping {
		r.proc.Kill()
		r.finish(Result{How: r.how})
		return
	}
	r.finish(Result{How: HowExit, ExitCode: exitCode})
}

// Timer tells the runner that the timer with this number ran out.
func (r *Runner) Timer(number int) {
	if number != r.timer {
		return
	}
	switch r.state {
	case Running:
		if r.timeUp {
			// The AI is not asked: there is no more time to give.
			r.kill(HowKilledTimeUp)
			return
		}
		r.sender.StillRunning(r.req.ID)
		r.state = Asked
		r.startTimer(r.settings.QuestionWait)
	case Asked:
		r.kill(HowKilledNoAnswer)
	case Stopping:
		r.proc.Kill()
	}
}

// Wait is the AI's answer "wait more".
func (r *Runner) Wait() {
	if r.state == Asked && r.timeUp {
		r.kill(HowKilledTimeUp)
		return
	}
	if r.state == Asked {
		r.state = Running
		r.startTimer(r.settings.TimeLimit)
	}
}

// Stop is the AI's answer "stop".
func (r *Runner) Stop() { r.kill(HowKilledByAI) }

// CtrlC is one Ctrl+C at the terminal.
func (r *Runner) CtrlC() { r.kill(HowKilledByUser) }

// Cancel tells the runner that the AI hung up on the request with this ID.
func (r *Runner) Cancel(id string) {
	if id == r.req.ID {
		r.kill(HowKilledAIGone)
	}
}

// TimeUp tells the runner that the session's time is up. A command that
// runs is not killed now. It gets no more "wait more": it is killed at its
// next time limit, or when the AI answers "wait".
func (r *Runner) TimeUp() {
	if r.state == Running || r.state == Asked {
		r.timeUp = true
	}
}

// End tells the runner that the session is over.
func (r *Runner) End() { r.kill(HowKilledSessionEnded) }

func (r *Runner) kill(how string) {
	if r.state != Running && r.state != Asked {
		return
	}
	r.how = how
	r.state = Stopping
	r.proc.Stop()
	r.startTimer(r.settings.KillWait)
}

// startTimer makes every timer started before it stale.
func (r *Runner) startTimer(d time.Duration) {
	r.timer++
	r.timers.Start(d, r.timer)
}

func (r *Runner) output(s string) {
	if s == "" {
		return
	}
	r.screen.Output(s)
	r.sender.Output(r.req.ID, s)
}

func (r *Runner) finish(result Result) {
	result.BytesReplaced = r.text.replaced
	req := r.req
	r.req = request.Request{}
	r.proc = nil
	r.how = ""
	r.timeUp = false
	r.timer++
	r.state = Idle
	r.screen.Ended(req, result)
	r.sender.Ended(req.ID, result)
	r.gate.Done()
}

// RequestID gives the ID of the request that runs, or "" when none does.
func (r *Runner) RequestID() string { return r.req.ID }
