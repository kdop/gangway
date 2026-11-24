// Package network puts the controller on the Tailscale network, from
// inside the program, and opens the door for the hosts there. The design
// is in docs/design.md.
package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"

	"github.com/kdop/gangway/shared/authkey"
	"github.com/kdop/gangway/shared/messages"
)

// Program names the folders of the controller and its login.
const Program = "gangway-controller"

// closeWait is how long Close waits for Tailscale before it gives up.
const closeWait = 3 * time.Second

// ErrKeyRefused says that Tailscale did not take the auth key.
var ErrKeyRefused = errors.New("Tailscale refused the auth key: make a new key and run \"" + Program + " login\" again")

// StateDir tells where the controller's Tailscale identity is kept.
func StateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, Program), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", Program), nil
}

// Config is what the controller needs to join.
type Config struct {
	KeyFile     string        // the auth key file
	StateDir    string        // where the identity is kept
	Name        string        // CONTROLLER_TAILSCALE_NAME
	JoinTimeout time.Duration // how long the join may take
	ControlURL  string        // empty: Tailscale's own servers
}

// Network is the controller on the Tailscale network.
type Network struct {
	server *tsnet.Server
}

// Join puts the controller on the Tailscale network. The identity is kept
// in the state folder, so the device is the same after a restart.
func Join(ctx context.Context, c Config) (*Network, error) {
	key, err := authkey.Load(c.KeyFile, Program)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return nil, err
	}
	// A folder that was there before may be open to others.
	if err := os.Chmod(c.StateDir, 0o700); err != nil {
		return nil, err
	}
	// The library would send its own log lines to Tailscale's servers.
	envknob.SetNoLogsNoSupport()

	server := &tsnet.Server{
		Dir:        c.StateDir,
		Hostname:   c.Name,
		AuthKey:    key,
		ControlURL: c.ControlURL,
		Logf:       logger.Discard,
		UserLogf:   logger.Discard,
	}
	ctx, stop := context.WithTimeout(ctx, c.JoinTimeout)
	defer stop()
	if _, err := server.Up(ctx); err != nil {
		timedOut := errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
		text := strings.ReplaceAll(err.Error(), key, "<the auth key>")
		closeWithin(server)
		switch {
		case timedOut:
			return nil, fmt.Errorf("could not get onto the Tailscale network in %s: check the network of this machine", c.JoinTimeout)
		case strings.Contains(strings.ToLower(text), "key"):
			// The library gives the refusal as text only, such as
			// "invalid authkey".
			return nil, fmt.Errorf("%w. Tailscale said: %s", ErrKeyRefused, authkey.Plain(text))
		default:
			return nil, fmt.Errorf("could not get onto the Tailscale network: %s", text)
		}
	}
	return &Network{server: server}, nil
}

// Listen opens the door for the hosts. It answers on the Tailscale
// network only: the library gives no other kind of listener.
func (n *Network) Listen() (net.Listener, error) {
	return n.server.Listen("tcp", ":"+strconv.Itoa(messages.HostDoorPort))
}

// Close takes the controller off the network. The identity stays on disk.
func (n *Network) Close() { closeWithin(n.server) }

func closeWithin(server *tsnet.Server) {
	done := make(chan struct{})
	go func() {
		server.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
}
