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
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/messages"
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

// Handler serves the API and the door for the cli-tools.
func (c *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(messages.HostPath, c.Door)
	mux.Handle("/", api.New(c.Store))
	return mux
}

// Run is the whole program. It gives the exit code.
func Run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("gangway-controller", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", messages.DefaultListen, "the address to listen on")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := CheckListen(*listen); err != nil {
		fmt.Fprintln(stderr, "gangway-controller:", err)
		return 2
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(stderr, "gangway-controller:", err)
		return 1
	}
	c := New()
	server := &http.Server{Handler: c.Handler(), ReadHeaderTimeout: 10 * time.Second}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		c.Door.EndAll(messages.EndControllerStopped)
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		server.Shutdown(ctx)
		server.Close()
	}()

	fmt.Fprintln(stderr, "gangway-controller: listening on", ln.Addr())
	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "gangway-controller:", err)
		return 1
	}
	return 0
}
