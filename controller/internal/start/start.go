// Package start checks where the controller may listen, puts the pieces
// together and runs them. The design is in design.md.
package start

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kdop/gangway/controller/internal/api"
	"github.com/kdop/gangway/controller/internal/hostdoor"
	"github.com/kdop/gangway/controller/internal/network"
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/authkey"
	"github.com/kdop/gangway/shared/messages"
	"github.com/kdop/gangway/shared/rawterm"
)

// ErrNotLocal is why the controller refuses an address.
var ErrNotLocal = errors.New("no login is set up, so the controller answers on this machine only: use 127.0.0.1, ::1 or localhost")

// CheckListen refuses every address that another machine could reach.
func CheckListen(addr string) error {
	hostName, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not an address of the form host:port", addr)
	}
	if hostName == "localhost" {
		return nil
	}
	if ip := net.ParseIP(hostName); ip != nil && ip.IsLoopback() {
		return nil
	}
	return ErrNotLocal
}

// Controller is the pieces put together.
type Controller struct {
	Store *sessions.Store
	Door  *hostdoor.Door
}

func New() *Controller {
	store := sessions.NewStore()
	return &Controller{Store: store, Door: &hostdoor.Door{Store: store}}
}

// Handler serves the API and the door for the cli-tools on one listener.
// It is for the one-machine mode.
func (c *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(messages.HostPath, c.Door)
	mux.Handle("/", api.New(c.Store))
	return mux
}

// APIHandler serves the API only. The door for the cli-tools is not on it.
func (c *Controller) APIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(messages.HostPath, http.NotFoundHandler())
	mux.Handle("/", api.New(c.Store))
	return mux
}

// HostHandler serves the door for the cli-tools only. The API is not on it.
func (c *Controller) HostHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(messages.HostPath, c.Door)
	return mux
}

// Usage is shown when the words after the program's name make no sense.
const Usage = `How to start gangway-controller:
  gangway-controller [options]         start, with the hosts' door on Tailscale
  gangway-controller login             store the Tailscale auth key of the controller, once
  gangway-controller -local [options]  start with the hosts' door on this machine, with no Tailscale`

// Run is the whole program. It gives the exit code.
func Run(args []string, stderr io.Writer) int {
	return run(context.Background(), args, stderr)
}

// run is Run, and ends when ctx does.
func run(ctx context.Context, args []string, stderr io.Writer) int {
	fail := func(code int, err error) int {
		fmt.Fprintln(stderr, "gangway-controller:", err)
		return code
	}
	if len(args) > 0 && args[0] == "login" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, Usage)
			return 2
		}
		return runLogin(ctx, os.Stdin, os.Stdout, fail)
	}
	flags := flag.NewFlagSet("gangway-controller", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, Usage); flags.PrintDefaults() }
	listen := flags.String("listen", messages.DefaultListen, "the address the API listens on")
	local := flags.Bool("local", false, "the one-machine mode: take hosts on localhost, with no Tailscale")
	public := flags.String("public-address", "", "the API address given to the AI. Empty: the address the API listens on")
	name := flags.String("tailscale-name", messages.DefaultControllerName, "the controller's name on the Tailscale network")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(stderr, Usage)
		return 2
	}
	if err := CheckListen(*listen); err != nil {
		return fail(2, err)
	}
	if *public != "" && !messages.PlainWebAddress(*public) {
		return fail(2, errors.New("-public-address must be a plain web address, such as https://gangway.example.com"))
	}

	c := New()
	var servers []*http.Server
	var listeners []net.Listener
	var tailnet *network.Network
	closeAll := func() {
		for _, ln := range listeners {
			ln.Close()
		}
		if tailnet != nil {
			tailnet.Close()
		}
	}

	// Tailscale comes first: with no auth key nothing may listen.
	if !*local {
		config, err := tailscaleConfig(*name)
		if err != nil {
			return fail(1, err)
		}
		if tailnet, err = network.Join(ctx, config); err != nil {
			return fail(1, err)
		}
		door, err := tailnet.Listen()
		if err != nil {
			closeAll()
			return fail(1, err)
		}
		listeners = append(listeners, door)
		servers = append(servers, &http.Server{Handler: c.HostHandler(), ReadHeaderTimeout: 10 * time.Second})
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		closeAll()
		return fail(1, err)
	}
	listeners = append(listeners, ln)
	apiHandler := c.APIHandler()
	if *local {
		apiHandler = c.Handler()
	}
	servers = append(servers, &http.Server{Handler: apiHandler, ReadHeaderTimeout: 10 * time.Second})

	c.Door.APIAddress = *public
	if c.Door.APIAddress == "" {
		c.Door.APIAddress = "http://" + ln.Addr().String()
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
		case <-ctx.Done():
		}
		c.Door.EndAll(messages.EndControllerStopped)
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		for _, server := range servers {
			server.Shutdown(ctx)
			server.Close()
		}
	}()

	fmt.Fprintln(stderr, "gangway-controller: the API listens on", ln.Addr())
	if *local {
		fmt.Fprintln(stderr, "gangway-controller: the one-machine mode is on: any program on this machine can start a session")
	} else {
		fmt.Fprintf(stderr, "gangway-controller: the hosts' door is on Tailscale, as %s, port %d\n", *name, messages.HostDoorPort)
	}
	failed := make(chan error, len(servers))
	for i, server := range servers {
		go func() { failed <- server.Serve(listeners[i]) }()
	}
	code := 0
	for range servers {
		if err := <-failed; err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "gangway-controller:", err)
			code = 1
			for _, server := range servers {
				server.Close()
			}
		}
	}
	if tailnet != nil {
		tailnet.Close()
	}
	return code
}

// joinTimeout is how long the controller waits to get onto Tailscale.
const joinTimeout = 30 * time.Second

func tailscaleConfig(name string) (network.Config, error) {
	keyFile, err := authkey.File(network.Program)
	if err != nil {
		return network.Config{}, err
	}
	stateDir, err := network.StateDir()
	if err != nil {
		return network.Config{}, err
	}
	return network.Config{KeyFile: keyFile, StateDir: stateDir, Name: name, JoinTimeout: joinTimeout}, nil
}

// runLogin is "gangway-controller login": it stores the auth key and
// tries it.
func runLogin(ctx context.Context, stdin, stdout *os.File, fail func(int, error) int) int {
	in := int(stdin.Fd())
	if !rawterm.IsTerminal(in) {
		return fail(1, errors.New("login needs a terminal: the auth key is pasted and must not be shown"))
	}
	config, err := tailscaleConfig(messages.DefaultControllerName)
	if err != nil {
		return fail(1, err)
	}
	// The terminal stops showing what is typed before the question is
	// asked: a key pasted at once would be shown otherwise.
	restore, err := rawterm.MakeRaw(in)
	if err != nil {
		return fail(1, err)
	}
	fmt.Fprint(stdout, "Paste the Tailscale auth key of the controller and press Enter. It is not shown: ")
	key, err := authkey.ReadHidden(stdin)
	restore()
	fmt.Fprintln(stdout)
	if err != nil {
		return fail(1, err)
	}
	if err := Login(ctx, config, key, stdout); err != nil {
		return fail(1, err)
	}
	return 0
}

// Login stores the key and tries it. A key that Tailscale refuses is not
// kept: the key from before, if there was one, is put back.
func Login(ctx context.Context, config network.Config, key string, screen io.Writer) error {
	old, oldErr := authkey.Load(config.KeyFile, network.Program)
	if err := authkey.Save(config.KeyFile, key); err != nil {
		return err
	}
	fmt.Fprintln(screen, "Trying the key on Tailscale ...")
	tailnet, err := network.Join(ctx, config)
	switch {
	case err == nil:
		tailnet.Close()
		fmt.Fprintln(screen, "The controller is ready. Start it with: gangway-controller")
		return nil
	case errors.Is(err, network.ErrKeyRefused):
		if oldErr == nil {
			if err := authkey.Save(config.KeyFile, old); err != nil {
				return err
			}
			return errors.New("Tailscale refused the key. The key from before is still the one stored")
		}
		if err := authkey.Remove(config.KeyFile); err != nil {
			return err
		}
		return errors.New("Tailscale refused the key. No key is stored")
	default:
		fmt.Fprintln(screen, "The key is stored, but it could not be tried:", err)
		return nil
	}
}
