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
	"github.com/kdop/gangway/cli/internal/login"
	"github.com/kdop/gangway/cli/internal/loop"
	"github.com/kdop/gangway/cli/internal/network"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/secrets"
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

// What the first words after the program's name ask for.
const (
	doSession = iota // a session over Tailscale
	doLocal          // a session in the one-machine mode
	doLogin          // store the Tailscale auth key
)

// Usage is shown when the words after the program's name make no sense.
const Usage = `How to start gangway:
  gangway                    start a session
  gangway login              store the Tailscale auth key of this host, once
  gangway --local [address]  start a session with a controller on this machine, with no Tailscale`

// Plan reads the words after the program's name. address is the
// controller's address in the one-machine mode, empty for its starting
// address.
func Plan(args []string) (what int, address string, err error) {
	switch {
	case len(args) == 0:
		return doSession, "", nil
	case len(args) == 1 && args[0] == "login":
		return doLogin, "", nil
	case len(args) == 1 && args[0] == "--local":
		return doLocal, "", nil
	case len(args) == 2 && args[0] == "--local":
		return doLocal, args[1], nil
	}
	return 0, "", errors.New(Usage)
}

// Run is the whole program. args are the words after the program's name.
// It gives the exit code.
func Run(args []string, stdin, stdout *os.File, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, "gangway:", err)
		return 1
	}
	what, address, err := Plan(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	in, out := int(stdin.Fd()), int(stdout.Fd())
	check := func() error {
		return Check(os.Geteuid(), terminal.IsTerminal(in), terminal.IsTerminal(out))
	}
	if what == doLogin {
		return runLogin(check, in, stdin, stdout, fail)
	}
	if err := check(); err != nil {
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
	host, _ := os.Hostname()

	var road network.Network
	if what == doLocal {
		if road, err = network.Local(address); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "Connecting to %s ...\n", road.Address())
	} else {
		if road, err = tailscaleRoad(s, host); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "Joining Tailscale ...")
	}
	if err := road.Join(context.Background()); err != nil {
		return fail(err)
	}
	// Runs last, after the terminal is put back.
	defer road.Leave()

	sess, err := session.New(sessionKeyBits)
	if err != nil {
		return fail(err)
	}

	// The events wait here for the loop. Nothing that posts may be stopped
	// by a slow loop for long, so there is room.
	ev := make(events, 1024)
	var l *loop.Loop

	conn, apiAddress, err := connection.Dial(context.Background(), road.Client(), road.Address(), connection.Start{
		KeyHash: sess.KeyHash(),
		Host:    host,
		Limits: messages.Limits{
			CommandSeconds: int(s.CommandTimeLimit.Seconds()),
			AnswerSeconds:  int(s.QuestionWait.Seconds()),
			MaxWaiting:     s.MaxWaitingRequests,
		},
	}, handler{ev, &l})
	if err != nil {
		return fail(fmt.Errorf("cannot reach the controller at %s: %w", road.Address(), err))
	}
	if err := sess.SetAddress(apiAddress); err != nil {
		conn.Close(messages.EndUser)
		return fail(err)
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

// tailscaleRoad gives the road over Tailscale, with the auth key of login.
func tailscaleRoad(s settings.Settings, host string) (network.Network, error) {
	file, err := secrets.File()
	if err != nil {
		return nil, err
	}
	key, err := secrets.Load(file)
	if err != nil {
		return nil, err
	}
	return network.Tailscale(network.Config{
		AuthKey:        key,
		Host:           host,
		ControllerName: s.ControllerName,
		JoinTimeout:    s.JoinTimeout,
	}), nil
}

// runLogin is "gangway login".
func runLogin(check func() error, in int, stdin, stdout *os.File, fail func(error) int) int {
	if err := check(); err != nil {
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
	file, err := secrets.File()
	if err != nil {
		return fail(err)
	}
	host, _ := os.Hostname()
	// Raw mode: what is pasted is not shown.
	restore, err := terminal.MakeRaw(in)
	if err != nil {
		return fail(err)
	}
	err = login.Run(context.Background(), login.Parts{
		Check:  check,
		Keys:   stdin,
		Screen: stdout,
		File:   file,
		Try: func(ctx context.Context, key string) error {
			road := network.Tailscale(network.Config{AuthKey: key, Host: host, ControllerName: s.ControllerName, JoinTimeout: s.JoinTimeout})
			defer road.Leave()
			return road.Join(ctx)
		},
	})
	restore()
	if err != nil {
		return fail(err)
	}
	return 0
}
