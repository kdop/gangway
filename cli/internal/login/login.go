// Package login stores the Tailscale auth key, once, and tries it. The
// design is in docs/design.md.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/kdop/gangway/cli/internal/network"
	"github.com/kdop/gangway/cli/internal/secrets"
	"github.com/kdop/gangway/shared/authkey"
)

// Parts are what a login needs.
type Parts struct {
	// Check refuses root and a missing terminal, as at a normal start.
	Check func() error
	// Keys gives what is typed. It must not be shown on the screen.
	Keys io.Reader
	// Screen is where the cli-tool writes.
	Screen io.Writer
	// File is the auth key file.
	File string
	// Try joins Tailscale with the key and leaves again.
	Try func(ctx context.Context, key string) error
}

// ErrStopped says the user pressed Ctrl+C at the question.
var ErrStopped = authkey.ErrStopped

// Run asks for the key, stores it and tries it.
func Run(ctx context.Context, p Parts) error {
	if err := p.Check(); err != nil {
		return err
	}
	fmt.Fprint(p.Screen, "  Paste the Tailscale auth key and press Enter. It is not shown: ")
	key, err := authkey.ReadHidden(p.Keys)
	fmt.Fprintln(p.Screen)
	if err != nil {
		return err
	}

	old, oldErr := secrets.Load(p.File)
	if err := secrets.Save(p.File, key); err != nil {
		return err
	}
	fmt.Fprintln(p.Screen, "  Trying the key on Tailscale ...")
	switch err := p.Try(ctx, key); {
	case err == nil:
		fmt.Fprintln(p.Screen, "  This host is ready. Start a session with: gangway")
		return nil
	case errors.Is(err, network.ErrKeyRefused):
		// A key that Tailscale refuses is not kept.
		if oldErr == nil {
			if err := secrets.Save(p.File, old); err != nil {
				return err
			}
			return errors.New("Tailscale refused the key. The key from before is still the one stored")
		}
		if err := secrets.Remove(p.File); err != nil {
			return err
		}
		return errors.New("Tailscale refused the key. No key is stored")
	default:
		fmt.Fprintln(p.Screen, "  The key is stored, but it could not be tried:", err)
		return nil
	}
}
