// Package terminal is the only piece that reads keys and writes to the
// screen. The design is in docs/design.md, the wording in docs/ux.md.
package terminal

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kdop/gangway/cli/internal/checks"
	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/cli/internal/session"
	"github.com/kdop/gangway/shared/messages"
)

const (
	question = "run it?   y yes   n no "

	// mark stands in for a character that must not reach the screen.
	mark = '�'
)

// Events is where the terminal sends what the user did.
type Events interface {
	// Key is one key press that is not Ctrl+C, as the bytes the key sent.
	Key(key []byte)
	// CtrlC is one Ctrl+C.
	CtrlC()
	// Quit is the second of two Ctrl+C close together.
	Quit()
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Terminal is not safe for use from more than one goroutine: the one loop
// of the cli-tool owns it.
type Terminal struct {
	out    io.Writer
	clock  Clock
	window time.Duration
	events Events
	look   Look
	left   func() (time.Duration, bool)

	keys      parser
	lastCtrlC time.Time
	armed     bool // a Ctrl+C came that a second one can pair with

	midLine bool             // the cursor is not at the start of a line
	blank   bool             // the last line written is empty
	prompt  *request.Request // the request whose prompt waits on screen
	extend  *extend          // the question to extend that waits on screen

	number int // the number of the last request that got a prompt
	lines  int // the lines the running command has written
}

// New makes a terminal that writes to out, in the plain look. window is
// DOUBLE_CTRL_C_WINDOW.
func New(out io.Writer, clock Clock, window time.Duration, events Events) *Terminal {
	return &Terminal{out: out, clock: clock, window: window, events: events}
}

// Dress sets the look of everything written from now on.
func (t *Terminal) Dress(look Look) { t.look = look }

// TimeLeft sets where the terminal asks how long the session still lasts.
// left tells false when there is nothing to tell.
func (t *Terminal) TimeLeft(left func() (time.Duration, bool)) { t.left = left }

// Input takes bytes read from the keyboard and sends the key presses in
// them to Events.
func (t *Terminal) Input(chunk []byte) {
	for _, k := range t.keys.feed(chunk) {
		if !k.ctrlC {
			t.events.Key(k.bytes)
			continue
		}
		now := t.clock.Now()
		if t.armed && now.Sub(t.lastCtrlC) <= t.window {
			t.armed = false
			t.events.Quit()
			continue
		}
		t.armed = true
		t.lastCtrlC = now
		t.events.CtrlC()
	}
}

// Start is what the screen shows when a session begins.
type Start struct {
	Host    string // the name of this host
	Network string // how the controller is reached
	Length  time.Duration
	Max     time.Duration // SESSION_MAX. Zero: no maximum
	Asked   time.Duration // the length before the maximum cut it. Zero: it was not cut
	Admin   string        // the admin file that holds the maximum
	Log     string        // the log file. Empty: nothing is logged
	Address string
	Key     string
}

// Start shows the session: its host, its length, its log, and the line the
// user pastes to the AI.
func (t *Terminal) Start(s Start) {
	t.gap()
	if s.Host != "" {
		t.line(t.look.row("host", t.text(s.Host)))
	}
	switch {
	case s.Max > 0 && s.Length >= s.Max:
		t.line(t.look.row("session", Short(s.Length)+", the maximum: then it ends"))
	case s.Max > 0:
		t.line(t.look.row("session", Short(s.Length)+", then you are asked to extend, up to the maximum of "+Short(s.Max)))
	default:
		t.line(t.look.row("session", Short(s.Length)+", then you are asked to extend"))
	}
	if s.Asked > 0 {
		t.line(under(Short(s.Asked) + " is above the maximum: it was cut to " + Short(s.Max)))
		t.line(under("the maximum is set in " + t.text(s.Admin)))
	}
	if s.Network != "" {
		t.line(t.look.row("network", t.text(s.Network)))
	}
	if s.Log != "" {
		t.line(t.look.row("log", t.text(s.Log)))
	}
	t.gap()
	t.line("  Paste this line to the AI:")
	t.gap()
	// At the start of the line: a copy of the whole line must start with
	// the slash, or an AI tool does not take it as a command.
	t.line("/gangway " + clean(s.Address) + " " + clean(s.Key))
	t.gap()
	key := func(k string) string { return t.look.paint(bold, k) }
	t.line("  " + key("y") + " run   " + key("n") + " refuse   " + key("Ctrl+C") + " end")
}

// Prompt shows a request and asks for the y or n.
func (t *Terminal) Prompt(req request.Request) {
	t.prompt = &req
	t.number++
	t.drawPrompt()
}

func (t *Terminal) title() string { return "request " + strconv.Itoa(t.number) }

func (t *Terminal) drawPrompt() {
	right := ""
	if t.left != nil {
		if left, ok := t.left(); ok && left > 0 {
			right = about(left) + " left"
		}
	}
	t.gap()
	t.line(t.look.rule(right, t.title()))
	t.gap()
	for _, l := range strings.Split(clean(t.prompt.Command), "\n") {
		t.line(commandMark + l)
	}
	t.gap()
	t.write("  " + question)
}

// Answer shows how the prompt was closed.
func (t *Terminal) Answer(req request.Request, answer gate.Answer) {
	t.prompt = nil
	switch answer {
	case gate.Yes:
		t.lines = 0
		t.gap()
		t.line(t.look.rule("Ctrl+C stops it", t.title(), "running"))
		t.gap()
	case gate.No:
		t.line("  " + t.look.bad() + " refused, nothing ran")
	case gate.AIGone:
		t.line("  " + t.look.bad() + " the AI hung up, nothing ran")
	case gate.TimeUp:
		t.line("  " + t.look.bad() + " the session's time is up, nothing ran")
	}
}

// extend is one question to extend.
type extend struct {
	length time.Duration
	toMax  bool
	until  time.Time // when the question stops waiting
}

// ExtendQuestion says that the session's time is up and asks whether to
// extend it by this length. toMax: that stretch ends at the maximum. wait
// is how long the question waits for an answer.
func (t *Terminal) ExtendQuestion(length time.Duration, toMax bool, wait time.Duration) {
	t.extend = &extend{length, toMax, t.clock.Now().Add(wait)}
	t.drawExtend()
}

func (t *Terminal) drawExtend() {
	right := ""
	if wait := wholeSeconds(t.extend.until.Sub(t.clock.Now())); wait > 0 {
		right = "answer in " + strconv.FormatInt(int64(wait/time.Second), 10) + " s"
	}
	t.gap()
	t.line(t.look.rule(right, "time is up"))
	t.gap()
	if t.extend.toMax {
		t.write("  extend by " + Short(wholeSeconds(t.extend.length)) + ", up to the maximum?   y yes   n no ")
		return
	}
	t.write("  extend by " + Short(t.extend.length) + "?   y yes   n no ")
}

// ExtendAnswer shows how the question to extend was closed. With Yes,
// length is the stretch the session got, and toMax tells that it ends at
// the maximum.
func (t *Terminal) ExtendAnswer(answer session.Answer, length time.Duration, toMax bool) {
	if t.extend == nil {
		return
	}
	t.extend = nil
	switch answer {
	case session.Yes:
		if toMax {
			t.line("  " + t.look.good() + " extended by " + Short(wholeSeconds(length)) + ", up to the maximum: then the session ends")
			return
		}
		t.line("  " + t.look.good() + " extended by " + Short(length))
	case session.AtMax:
		if t.midLine {
			t.write("\n")
		}
	case session.No:
		t.line("  " + t.look.bad() + " not extended")
	case session.NoAnswer:
		t.line("  " + t.look.bad() + " no answer in time, not extended")
	}
}

// Output shows a piece of what a command wrote, cleaned, with the output
// mark at the start of every line.
func (t *Terminal) Output(text string) {
	var b strings.Builder
	midLine := t.midLine
	for _, r := range clean(text) {
		if !midLine {
			b.WriteString(t.look.outputMark())
			midLine = true
		}
		b.WriteRune(r)
		if r == '\n' {
			midLine = false
			t.lines++
		}
	}
	t.write(b.String())
}

// Ended shows how a command ended.
func (t *Terminal) Ended(req request.Request, result runner.Result) {
	if t.midLine {
		t.lines++
	}
	t.gap()
	killed := func(why string) { t.line("  " + t.look.bad() + " killed: " + why) }
	switch result.How {
	case messages.HowExit:
		wrote := "no output"
		switch {
		case t.lines == 1:
			wrote = "1 line sent to the AI"
		case t.lines > 1:
			wrote = strconv.Itoa(t.lines) + " lines sent to the AI"
		}
		t.line("  " + t.look.good() + " ended, exit " + strconv.Itoa(result.ExitCode) + ", " + wrote)
	case messages.HowKilledByUser:
		killed("you pressed Ctrl+C")
	case messages.HowKilledByAI:
		killed("the AI said stop")
	case messages.HowKilledNoAnswer:
		killed("the time limit passed and the AI gave no answer")
	case messages.HowKilledTimeUp:
		killed("the time limit passed and the session's time was up")
	case messages.HowKilledAIGone:
		killed("the AI hung up")
	case messages.HowKilledSessionEnded:
		killed("the session ended")
	case messages.HowStartFailed:
		t.line("  " + t.look.bad() + " the command could not be started")
	default:
		t.line("  " + t.look.bad() + " ended: " + clean(result.How))
	}
	t.lines = 0
}

// Refused shows that a request was refused before any prompt. It takes the
// reason only, so the refused text cannot reach the screen.
func (t *Terminal) Refused(reason string) {
	why := clean(strings.ReplaceAll(reason, "\n", " "))
	switch reason {
	case messages.ReasonControlCharacter:
		why = "it holds a character that is not allowed"
	case messages.ReasonLineFull:
		why = "too many requests wait"
	case messages.ReasonTimeUp:
		why = "the session's time is up"
	}
	t.line("  " + t.look.bad() + " refused a request with no prompt: " + why)
	t.again()
}

// Note shows one line from the cli-tool itself. A prompt that waits is
// shown again under it, so the prompt is always the last thing on screen.
func (t *Terminal) Note(text string) {
	t.line("  " + t.text(text))
	t.again()
}

func (t *Terminal) again() {
	if t.prompt != nil {
		t.drawPrompt()
	}
	if t.extend != nil {
		t.drawExtend()
	}
}

// Over is what the screen shows when a session has ended.
type Over struct {
	Why     string // why the session ended
	Lasted  time.Duration
	Ran     int    // the requests that were approved
	Refused int    // the requests that were refused, by anyone
	Log     string // the log file. Empty: nothing was logged
	LogOld  bool   // the log has older files beside this one
	LogBad  bool   // the log could not be written to the end
}

// Over shows the end of the session as the last thing on screen.
func (t *Terminal) Over(o Over) {
	t.prompt, t.extend = nil, nil
	t.gap()
	t.line(t.look.rule("", "session over"))
	t.gap()
	if o.Why != "" {
		t.line(t.look.row("ended", t.text(o.Why)))
	}
	t.line(t.look.row("lasted", about(o.Lasted)))
	requests := "none"
	if o.Ran > 0 || o.Refused > 0 {
		requests = strconv.Itoa(o.Ran) + " run, " + strconv.Itoa(o.Refused) + " refused"
	}
	t.line(t.look.row("requests", requests))
	if o.Log != "" {
		t.line(t.look.row("log", t.text(o.Log)))
		switch {
		case o.LogBad:
			t.line(under("The log is not whole: it could not be written."))
		case o.LogOld:
			t.line(under("That file holds the last part. The parts before it are in the same folder."))
		}
	}
	t.gap()
	t.line("  The key is dead.")
}

// Forget drops the prompt or the question that waits on screen, when the
// session ends with no answer to it. A later note then does not show it
// again.
func (t *Terminal) Forget() {
	if t.prompt != nil {
		t.line("  " + t.look.bad() + " the session ended, nothing ran")
	}
	if t.extend != nil && t.midLine {
		t.write("\n")
	}
	t.prompt, t.extend = nil, nil
}

// text makes a text from outside fit for one line of the screen.
func (t *Terminal) text(s string) string {
	return clean(strings.ReplaceAll(s, "\n", " "))
}

// gap leaves one empty line, and never two.
func (t *Terminal) gap() {
	if t.midLine {
		t.write("\n")
	}
	if !t.blank {
		t.write("\n")
	}
}

// line writes one line that starts at the start of a line.
func (t *Terminal) line(s string) {
	if t.midLine {
		t.write("\n")
	}
	t.write(s + "\n")
}

func (t *Terminal) write(s string) {
	if s == "" {
		return
	}
	io.WriteString(t.out, s)
	t.blank = s == "\n" && !t.midLine || strings.HasSuffix(s, "\n\n")
	t.midLine = !strings.HasSuffix(s, "\n")
}

// clean replaces every character that must not reach the screen. Line
// breaks and tabs stay.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if checks.Forbidden(r) {
			return mark
		}
		return r
	}, s)
}
