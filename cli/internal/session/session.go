// Package session makes the session key and the paste line, and runs the
// session's clock: the time is up, the question to extend, the maximum,
// the end.
// Nothing here is written to disk. The design is in design.md.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kdop/gangway/shared/messages"
)

// State is where the session stands.
type State int

const (
	Live      State = iota // requests are taken, and the clock counts to the end time
	TimeUp                 // the time is up, and the question waits for a command to end
	Extending              // the question to extend is on the screen
	Ended                  // the session is over
)

func (s State) String() string {
	switch s {
	case Live:
		return "Live"
	case TimeUp:
		return "TimeUp"
	case Extending:
		return "Extending"
	case Ended:
		return "Ended"
	}
	return "unknown"
}

// Answer is how the question to extend was closed, for the screen.
type Answer int

const (
	Yes      Answer = iota // the user pressed y
	No                     // the user pressed n or Ctrl+C
	NoAnswer               // nobody answered within EXTEND_WAIT
	AtMax                  // the maximum was reached while the question was shown
)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Timers calls the session's Timer event with the same number after d.
type Timers interface {
	Start(d time.Duration, number int)
}

// Screen is the terminal, as far as the session needs it.
type Screen interface {
	// ExtendQuestion asks whether to extend by this length. toMax: that
	// stretch ends at the maximum.
	ExtendQuestion(length time.Duration, toMax bool)
	// ExtendAnswer closes the question. With Yes, length is the stretch
	// the session got, and toMax tells that it ends at the maximum.
	ExtendAnswer(answer Answer, length time.Duration, toMax bool)
}

// Sender tells the controller about the session's time. The controller
// only notes it: it cannot answer.
type Sender interface {
	TimeUp()
	Extended(secondsLeft int)
}

// Loop is the loop of the cli-tool, as far as the session needs it.
type Loop interface {
	// TimeUp refuses what waits. Nothing new is taken until the user
	// extends.
	TimeUp()
	// NotExtended ends the session.
	NotExtended()
	// MaxReached ends the session: it has lasted SESSION_MAX.
	MaxReached()
}

// Edges are what the clock is handed from outside.
type Edges struct {
	Clock  Clock
	Timers Timers
	Screen Screen
	Sender Sender
	Loop   Loop

	Length     time.Duration // the length this session starts with
	Max        time.Duration // SESSION_MAX. Zero: no maximum
	ExtendWait time.Duration // EXTEND_WAIT
	MinShow    time.Duration // PROMPT_MIN_SHOW
}

// MinKeyBits is the least SESSION_KEY_BITS may be. A setting below it is
// refused, so a mistake in the settings cannot make a key that can be
// guessed.
const MinKeyBits = 128

// Session is one session of the cli-tool. It is not safe for use from more
// than one goroutine: the one loop of the cli-tool owns it.
type Session struct {
	address string
	key     string

	state   State
	edges   Edges
	clocked bool          // StartClock was called
	end     time.Time     // when the time is up
	maxEnd  time.Time     // when SESSION_MAX is reached. Zero: no maximum
	stretch time.Duration // the length of the stretch that counts
	toMax   bool          // the stretch that counts ends at the maximum
	maxWait bool          // the question's timer ends at the maximum, before EXTEND_WAIT
	timer   int           // the number of the timer that counts
	shownAt time.Time     // when the question was shown
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

// State tells where the session stands.
func (s *Session) State() State { return s.state }

// End ends the session. There is no way back.
func (s *Session) End() {
	s.state = Ended
	s.timer++
}

// StartClock starts the session's time. It is called once, when the
// session begins. Without it the session has no end time.
func (s *Session) StartClock(e Edges) {
	if s.clocked || s.state != Live {
		return
	}
	s.clocked = true
	s.edges = e
	if e.Max > 0 {
		// The only place the maximum is set. Nothing moves it later.
		s.maxEnd = s.today().Add(e.Max)
	}
	s.count()
}

// count starts a new stretch of the session's length. A stretch that
// would pass the maximum ends at the maximum.
func (s *Session) count() {
	now := s.today()
	s.stretch, s.toMax = s.next(now)
	s.end = now.Add(s.stretch)
	s.startTimer(s.stretch)
}

// next gives the stretch a session would get at this moment, and whether
// it ends at the maximum.
func (s *Session) next(now time.Time) (time.Duration, bool) {
	if s.maxEnd.IsZero() {
		return s.edges.Length, false
	}
	left := s.maxEnd.Sub(now)
	if left < 0 {
		left = 0
	}
	if left <= s.edges.Length {
		return left, true
	}
	return s.edges.Length, false
}

// today gives the time of day. Go's own way of measuring a stretch of
// time stands still while the machine sleeps, so the end time is held and
// compared as a time of day.
func (s *Session) today() time.Time { return s.edges.Clock.Now().Round(0) }

// startTimer makes every timer started before it stale.
func (s *Session) startTimer(d time.Duration) {
	s.timer++
	s.edges.Timers.Start(d, s.timer)
}

// Takes tells whether a new request may go on to the prompt. It also
// reads the clock: a timer that comes late, as on a machine that slept,
// must not let a request in after the end time.
func (s *Session) Takes() bool {
	if s.state == Live && s.clocked && !s.today().Before(s.end) {
		s.timeUp()
	}
	return s.state == Live
}

// Timer tells the session that the timer with this number ran out.
func (s *Session) Timer(number int) {
	if number != s.timer {
		return
	}
	switch s.state {
	case Live:
		s.timeUp()
	case Extending:
		if s.maxWait {
			s.edges.Screen.ExtendAnswer(AtMax, 0, false)
			s.edges.Loop.MaxReached()
			return
		}
		s.edges.Screen.ExtendAnswer(NoAnswer, 0, false)
		s.edges.Loop.NotExtended()
	}
}

func (s *Session) timeUp() {
	s.state = TimeUp
	s.timer++
	s.edges.Sender.TimeUp()
	s.edges.Loop.TimeUp()
}

// Idle tells the session that no command runs. The question to extend is
// shown only then. At the maximum there is no question: the session ends.
func (s *Session) Idle() {
	if s.state != TimeUp {
		return
	}
	now := s.today()
	length, toMax := s.next(now)
	if s.toMax || length <= 0 {
		s.edges.Loop.MaxReached()
		return
	}
	s.state = Extending
	s.edges.Screen.ExtendQuestion(length, toMax)
	s.shownAt = s.edges.Clock.Now()
	// The question does not wait past the maximum.
	wait := s.edges.ExtendWait
	s.maxWait = toMax && length < wait
	if s.maxWait {
		wait = length
	}
	s.startTimer(wait)
}

// Press is one key press, as the bytes the key sent. Only the question to
// extend takes one, and only y or n.
func (s *Session) Press(key []byte) {
	if s.state != Extending {
		return
	}
	if s.edges.Clock.Now().Sub(s.shownAt) < s.edges.MinShow {
		return
	}
	if len(key) != 1 {
		return
	}
	switch key[0] {
	case 'y':
		if length, _ := s.next(s.today()); length <= 0 {
			s.edges.Screen.ExtendAnswer(AtMax, 0, false)
			s.edges.Loop.MaxReached()
			return
		}
		// The only place a session gets more time.
		s.state = Live
		s.count()
		s.edges.Screen.ExtendAnswer(Yes, s.stretch, s.toMax)
		s.edges.Sender.Extended(Seconds(s.stretch))
	case 'n':
		s.no()
	}
}

// CtrlC is one Ctrl+C at the question to extend. It means no, and it is
// not held back by MinShow: it can only end.
func (s *Session) CtrlC() {
	if s.state == Extending {
		s.no()
	}
}

func (s *Session) no() {
	s.edges.Screen.ExtendAnswer(No, 0, false)
	s.edges.Loop.NotExtended()
}

// Seconds gives a length in whole seconds, rounded up, as it is told to
// the controller.
func Seconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// Address gives the controller's address, as the AI must call it.
func (s *Session) Address() string { return s.address }
