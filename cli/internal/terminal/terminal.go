// Package terminal is the only piece that reads keys and writes to the
// screen. The design is in design.md, the wording in ux.md.
package terminal

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kdop/gangway/cli/internal/checks"
	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/shared/messages"
)

const (
	// outputMark starts every line of a command's output. No line of the
	// cli-tool itself starts with it, or with a space.
	outputMark  = "  | "
	commandMark = "  > "
	question    = "Run it? y/n "

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

	keys      parser
	lastCtrlC time.Time
	armed     bool // a Ctrl+C came that a second one can pair with

	midLine bool             // the cursor is not at the start of a line
	prompt  *request.Request // the request whose prompt waits on screen
}

// New makes a terminal that writes to out. window is DOUBLE_CTRL_C_WINDOW.
func New(out io.Writer, clock Clock, window time.Duration, events Events) *Terminal {
	return &Terminal{out: out, clock: clock, window: window, events: events}
}

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

// Prompt shows a request and asks for the y or n.
func (t *Terminal) Prompt(req request.Request) {
	t.prompt = &req
	t.drawPrompt()
}

func (t *Terminal) drawPrompt() {
	t.line("")
	t.line("AI asks to run:")
	for _, l := range strings.Split(clean(t.prompt.Command), "\n") {
		t.line(commandMark + l)
	}
	t.write(question)
}

// Answer shows how the prompt was closed.
func (t *Terminal) Answer(req request.Request, answer gate.Answer) {
	t.prompt = nil
	switch answer {
	case gate.Yes:
		t.write("y\n")
	case gate.No:
		t.write("n\n")
	case gate.AIGone:
		t.line("The AI hung up. Nothing ran.")
	}
}

// Output shows a piece of what a command wrote, cleaned, with the output
// mark at the start of every line.
func (t *Terminal) Output(text string) {
	var b strings.Builder
	midLine := t.midLine
	for _, r := range clean(text) {
		if !midLine {
			b.WriteString(outputMark)
			midLine = true
		}
		b.WriteRune(r)
		if r == '\n' {
			midLine = false
		}
	}
	t.write(b.String())
}

// Ended shows how a command ended.
func (t *Terminal) Ended(req request.Request, result runner.Result) {
	switch result.How {
	case messages.HowExit:
		t.line(fmt.Sprintf("Done. Exit code %d.", result.ExitCode))
	case messages.HowKilledByUser:
		t.line("Killed: you pressed Ctrl+C.")
	case messages.HowKilledByAI:
		t.line("Killed: the AI said stop.")
	case messages.HowKilledNoAnswer:
		t.line("Killed: the time limit passed and the AI gave no answer.")
	case messages.HowKilledAIGone:
		t.line("Killed: the AI hung up.")
	case messages.HowKilledSessionEnded:
		t.line("Killed: the session ended.")
	case messages.HowStartFailed:
		t.line("The command could not be started.")
	default:
		t.line("Ended: " + clean(result.How) + ".")
	}
}

// Refused shows that a request was refused before any prompt. It takes the
// reason only, so the refused text cannot reach the screen.
func (t *Terminal) Refused(reason string) {
	switch reason {
	case messages.ReasonControlCharacter:
		t.Note("Refused a request with no prompt: it holds a character that is not allowed.")
	case messages.ReasonLineFull:
		t.Note("Refused a request with no prompt: too many requests wait.")
	default:
		t.Note("Refused a request with no prompt: " + reason + ".")
	}
}

// PasteLine shows the line the user pastes to the AI.
func (t *Terminal) PasteLine(address, sessionKey string) {
	t.Note("Paste this line to the AI:")
	t.line("")
	t.line("/gangway " + clean(address) + " " + clean(sessionKey))
	t.line("")
}

// Note shows one line from the cli-tool itself. A prompt that waits is
// shown again under it, so the prompt is always the last thing on screen.
func (t *Terminal) Note(text string) {
	t.line(clean(strings.ReplaceAll(text, "\n", " ")))
	if t.prompt != nil {
		t.drawPrompt()
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

// Forget drops the prompt that waits on screen, when the session ends
// with no answer to it. A later note then does not show it again.
func (t *Terminal) Forget() {
	if t.prompt != nil {
		t.prompt = nil
		t.line("")
	}
}
