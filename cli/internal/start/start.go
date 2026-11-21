// Package start is the beginning and the end of the program: the checks,
// the edges, the loop, the cleaning up. The design is in design.md.
package start

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kdop/gangway/cli/internal/connection"
	"github.com/kdop/gangway/cli/internal/loop"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/cli/internal/settings"
	"github.com/kdop/gangway/cli/internal/terminal"
	"github.com/kdop/gangway/shared/messages"
)

const sessionKeyBits = 256 // SESSION_KEY_BITS

// Check refuses to go on as root, or with no terminal.
func Check(userID int, inputIsTerminal, outputIsTerminal bool) error {
	if userID == 0 {
		return errors.New("gangway does not run as root: a command approved by mistake would then harm the whole host. Start it as a plain user")
	}
	if !inputIsTerminal || !outputIsTerminal {
		return errors.New("gangway needs a terminal: each command is approved by a key press, so it cannot run in the background or with its input or output sent elsewhere")
	}
	return nil
}

// events carries every event to the one loop.
type events chan func()

func (e events) post(f func()) { e <- f }

// handler hands what the connection hears over to the loop.
type handler struct {
	events events
	l      **loop.Loop
}

func (h handler) Request(req request.Request) { h.events.post(func() { (*h.l).Request(req) }) }
func (h handler) Wait(id string)              { h.events.post(func() { (*h.l).Wait(id) }) }
func (h handler) Stop(id string)              { h.events.post(func() { (*h.l).Stop(id) }) }
func (h handler) Cancel(id string)            { h.events.post(func() { (*h.l).Cancel(id) }) }
func (h handler) SessionEnd(reason string)    { h.events.post(func() { (*h.l).SessionEnd(reason) }) }
func (h handler) Lost(error)                  { h.events.post(func() { (*h.l).Lost() }) }

type timers struct {
	events events
	l      **loop.Loop
}

func (t timers) Start(d time.Duration, number int) {
	time.AfterFunc(d, func() { t.events.post(func() { (*t.l).Timer(number) }) })
}

type clock struct{}

func (clock) Now() time.Time { return time.Now() }

// Run is the whole program. It gives the exit code.
func Run(stdin, stdout *os.File, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "gangway:", err)
		return 1
	}
	in, out := int(stdin.Fd()), int(stdout.Fd())
	if err := Check(os.Geteuid(), terminal.IsTerminal(in), terminal.IsTerminal(out)); err != nil {
		return fail(err)
	}
	path, err := settings.UserFile()
	if err != nil {
		return fail(err)
	}
	s, err := settings.Load(path)
	if err != nil {
		return fail(err)
	}
	sess, err := session.New(s.ControllerAddress, sessionKeyBits)
	if err != nil {
		return fail(err)
	}
	host, _ := os.Hostname()

	// The events wait here for the loop. Nothing that posts may be stopped
	// by a slow loop for long, so there is room.
	ev := make(events, 1024)
	var l *loop.Loop

	fmt.Fprintf(stdout, "Connecting to %s ...\n", s.ControllerAddress)
	conn, err := connection.Dial(context.Background(), s.ControllerAddress, connection.Start{
		KeyHash: sess.KeyHash(),
		Host:    host,
		Limits: messages.Limits{
			CommandSeconds: int(s.CommandTimeLimit.Seconds()),
			AnswerSeconds:  int(s.QuestionWait.Seconds()),
			MaxWaiting:     s.MaxWaitingRequests,
		},
	}, handler{ev, &l})
	if err != nil {
		return fail(fmt.Errorf("cannot reach the controller at %s: %w", s.ControllerAddress, err))
	}

	restore, err := terminal.MakeRaw(in)
	if err != nil {
		conn.Close(messages.EndUser)
		return fail(err)
	}
	defer restore()

	l = loop.New(loop.Parts{
		Settings: s,
		Session:  sess,
		Conn:     conn,
		Screen:   stdout,
		Clock:    clock{},
		Starter: &runner.OSStarter{
			Out:  func(piece []byte) { ev.post(func() { l.Output(piece) }) },
			Exit: func(code int) { ev.post(func() { l.Exited(code) }) },
		},
		Timers: timers{ev, &l},
	})

	go readKeys(stdin, ev, &l)

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() {
		for range signals {
			ev.post(func() { l.End(messages.EndUser, "Told to stop from outside: the session ends.") })
		}
	}()

	l.Begin()
	for l.State() != loop.Over {
		(<-ev)()
	}
	return 0
}

// readKeys hands the bytes of the keyboard to the loop. When the keyboard
// is gone, the session ends.
func readKeys(stdin *os.File, ev events, l **loop.Loop) {
	buf := make([]byte, 256)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			ev.post(func() { (*l).Input(chunk) })
		}
		if err != nil {
			ev.post(func() { (*l).End(messages.EndUser, "The terminal is gone: the session ends.") })
			return
		}
	}
}
