// Package network is the road to the controller: Tailscale from inside
// the program, or localhost in the one-machine mode. The design is in
// design.md.
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"

	"github.com/kdop/gangway/shared/messages"
)

// leaveWait is how long Leave waits for Tailscale before it gives up.
const leaveWait = 3 * time.Second

// Network is one road to the controller.
type Network interface {
	// Join gets onto the road. It must be called once, before the others.
	Join(ctx context.Context) error
	// Client gives what opens a connection over this road.
	Client() *http.Client
	// Address gives the address of the controller on this road.
	Address() string
	// Leave gets off the road. It may be called more than once.
	Leave()
}

// Local gives the one-machine road: the controller on localhost. An empty
// address means the controller's starting address.
func Local(address string) (Network, error) {
	if address == "" {
		address = "http://" + messages.DefaultListen
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("the one-machine mode needs an address such as http://127.0.0.1:8642")
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("the one-machine mode is for localhost only, and %s is not localhost", u.Hostname())
	}
	return local{address: strings.TrimRight(address, "/")}, nil
}

type local struct{ address string }

func (local) Join(context.Context) error { return nil }
func (local) Client() *http.Client       { return http.DefaultClient }
func (l local) Address() string          { return l.address }
func (local) Leave()                     {}

// Config is what the Tailscale road needs.
type Config struct {
	AuthKey        string
	Host           string        // the host's name
	ControllerName string        // CONTROLLER_TAILSCALE_NAME
	JoinTimeout    time.Duration // JOIN_TIMEOUT
	Folder         string        // where the folder of this start is made. Empty: the system's place for short-lived files
	ControlURL     string        // empty: Tailscale's own servers
}

// Tailscale gives the road over Tailscale. Nothing is joined yet.
func Tailscale(c Config) Network { return &tailscale{config: c} }

type tailscale struct {
	config Config
	server *tsnet.Server
	dir    string
}

// ErrKeyRefused says that Tailscale did not take the auth key.
var ErrKeyRefused = errors.New("Tailscale refused the auth key: make a new key and run \"gangway login\" again")

func (t *tailscale) Join(ctx context.Context) error {
	// The library would send its own log lines to Tailscale's servers.
	envknob.SetNoLogsNoSupport()

	dir, err := os.MkdirTemp(t.config.Folder, "gangway-tailscale-")
	if err != nil {
		return err
	}
	// Only a device kept in memory is new at every start.
	store, err := mem.New(logger.Discard, "")
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	t.dir = dir
	t.server = &tsnet.Server{
		Dir:        dir,
		Store:      store,
		Hostname:   "gangway-" + deviceName(t.config.Host),
		Ephemeral:  true,
		AuthKey:    t.config.AuthKey,
		ControlURL: t.config.ControlURL,
		Logf:       logger.Discard,
		UserLogf:   logger.Discard,
	}
	ctx, stop := context.WithTimeout(ctx, t.config.JoinTimeout)
	defer stop()
	_, err = t.server.Up(ctx)
	if err == nil {
		return nil
	}
	timedOut := errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
	text := strings.ReplaceAll(err.Error(), t.config.AuthKey, "<the auth key>")
	t.Leave()
	switch {
	case timedOut:
		return fmt.Errorf("could not get onto the Tailscale network in %s: check the network of this host", t.config.JoinTimeout)
	case strings.Contains(strings.ToLower(text), "key"):
		// The library gives the refusal as text only, such as
		// "invalid authkey".
		return ErrKeyRefused
	default:
		return fmt.Errorf("could not get onto the Tailscale network: %s", text)
	}
}

func (t *tailscale) Client() *http.Client { return t.server.HTTPClient() }

func (t *tailscale) Address() string {
	return "http://" + net.JoinHostPort(t.config.ControllerName, strconv.Itoa(messages.HostDoorPort))
}

func (t *tailscale) Leave() {
	if t.server != nil {
		server := t.server
		t.server = nil
		done := make(chan struct{})
		go func() {
			server.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(leaveWait):
		}
	}
	if t.dir != "" {
		os.RemoveAll(t.dir)
		t.dir = ""
	}
}

// deviceName turns a host's name into one Tailscale takes: letters, digits
// and "-" only.
func deviceName(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	name := strings.TrimRight(b.String(), "-")
	if name == "" {
		return "host"
	}
	if len(name) > 50 {
		name = strings.TrimRight(name[:50], "-")
	}
	return name
}
