// Package session makes the session key and the paste line, and holds
// whether the session is live. Nothing here is written to disk.
// The design is in design.md.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/kdop/gangway/shared/messages"
)

// MinKeyBits is the least SESSION_KEY_BITS may be. A setting below it is
// refused, so a mistake in the settings cannot make a key that can be
// guessed.
const MinKeyBits = 128

// Session is one session of the cli-tool. It is not safe for use from more
// than one goroutine: the one loop of the cli-tool owns it.
type Session struct {
	address string
	key     string
	ended   bool
}

// New makes a live session with a fresh key from the system's random
// source. bits is SESSION_KEY_BITS. The session has no address yet.
func New(bits int) (*Session, error) {
	return newFrom(rand.Reader, bits)
}

func newFrom(random io.Reader, bits int) (*Session, error) {
	if bits < MinKeyBits || bits%8 != 0 {
		return nil, fmt.Errorf("SESSION_KEY_BITS is %d: it must be a multiple of 8 and at least %d", bits, MinKeyBits)
	}
	raw := make([]byte, bits/8)
	if _, err := io.ReadFull(random, raw); err != nil {
		return nil, fmt.Errorf("no random bytes for the session key: %w", err)
	}
	return &Session{key: messages.KeyPrefix + hex.EncodeToString(raw)}, nil
}

// ErrBadAddress says the controller gave an address that cannot go into
// the paste line. It does not hold the address: that text must not reach
// the screen.
var ErrBadAddress = errors.New("the controller gave an API address that cannot be used: it must be a plain web address")

// SetAddress takes the API address the controller gave, as the AI must
// call it. Anything but a plain web address is refused.
func (s *Session) SetAddress(address string) error {
	if !messages.PlainWebAddress(address) {
		return ErrBadAddress
	}
	s.address = address
	return nil
}

// Key is the session key. It goes to the user's screen in the paste line
// and nowhere else.
func (s *Session) Key() string { return s.key }

// KeyHash is the form of the key that goes to the controller.
func (s *Session) KeyHash() string { return messages.KeyHash(s.key) }

// PasteLine is the line the user gives to the AI, in the form of ux.md.
func (s *Session) PasteLine() string {
	return "/gangway " + s.address + " " + s.key
}

// Live tells whether the session may still take requests.
func (s *Session) Live() bool { return !s.ended }

// End ends the session. There is no way back.
func (s *Session) End() { s.ended = true }

// Address gives the controller's address, as the AI must call it.
func (s *Session) Address() string { return s.address }
