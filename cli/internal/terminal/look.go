package terminal

import (
	"io"
	"strconv"
	"strings"
	"time"
)

// Look is how the screen is dressed. It changes signs and colors only,
// never what a line says. The zero value is the plain look: ASCII, no
// color.
type Look struct {
	Unicode bool // lines, check marks and the output mark as Unicode signs
	Color   bool
	Width   int // the columns of the terminal. Zero: not known
}

// Detect picks the look from the surroundings. getenv reads one variable
// of the environment.
func Detect(getenv func(string) string, width int) Look {
	term := getenv("TERM")
	dumb := term == "" || term == "dumb"
	locale := getenv("LC_ALL")
	if locale == "" {
		locale = getenv("LC_CTYPE")
	}
	if locale == "" {
		locale = getenv("LANG")
	}
	locale = strings.ReplaceAll(strings.ToUpper(locale), "-", "")
	return Look{
		Unicode: !dumb && strings.Contains(locale, "UTF8"),
		Color:   !dumb && getenv("NO_COLOR") == "",
		Width:   width,
	}
}

// The only codes the cli-tool writes: each sets a color or a weight, and
// none moves the cursor or clears anything.
const (
	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	red   = "\x1b[31m"
	green = "\x1b[32m"
	cyan  = "\x1b[36m"
	reset = "\x1b[0m"
)

func (l Look) paint(code, s string) string {
	if !l.Color || s == "" {
		return s
	}
	return code + s + reset
}

func (l Look) pick(unicode, plain string) string {
	if l.Unicode {
		return unicode
	}
	return plain
}

// outputMark starts every line of a command's output, and commandMark
// every line of the command in a prompt. No line of the cli-tool itself
// starts with either, and neither is ever painted.
func (l Look) outputMark() string { return l.pick("  │ ", "  | ") }

const commandMark = "  > "

func (l Look) good() string { return l.paint(green, l.pick("✓", "+")) }
func (l Look) bad() string  { return l.paint(red, l.pick("✗", "x")) }

// ruleWidth is how wide a header line is, without what stands at its
// right.
const ruleWidth = 39

// rule gives a header line: the titles with a line between and after
// them, and a text at the right.
func (l Look) rule(right string, titles ...string) string {
	dash := l.pick("─", "-")
	s, width := "", 0
	for _, title := range titles {
		s += dash + dash + " " + title + " "
		width += 4 + len([]rune(title))
	}
	fill := ruleWidth - width
	if fill < 2 {
		fill = 2
	}
	s = "  " + l.paint(dim, s+strings.Repeat(dash, fill))
	if right != "" {
		s += "  " + right
	}
	return s
}

// row gives one line of a list: a name and its value.
func (l Look) row(name, value string) string {
	const width = 10
	if len(name) < width {
		name += strings.Repeat(" ", width-len(name))
	}
	return "  " + l.paint(dim, name) + value
}

// under gives a line that goes on under the value of a row.
func under(text string) string { return strings.Repeat(" ", 12) + text }

var banner = []string{
	"   ____",
	"  / ___| __ _ _ __   __ ___      ____ _ _   _",
	" | |  _ / _` | '_ \\ / _` \\ \\ /\\ / / _` | | | |",
	" | |_| | (_| | | | | (_| |\\ V  V / (_| | |_| |",
	"  \\____|\\__,_|_| |_|\\__, | \\_/\\_/ \\__,_|\\__, |",
	"                    |___/               |___/",
}

// Banner writes the name in large letters. It is left out on a terminal
// that is too narrow for it.
func Banner(out io.Writer, look Look) {
	widest := 0
	for _, line := range banner {
		if len(line) > widest {
			widest = len(line)
		}
	}
	if look.Width != 0 && look.Width <= widest {
		return
	}
	for _, line := range banner {
		io.WriteString(out, look.paint(cyan, line)+"\n")
	}
}

// Wait writes the line that shows while the cli-tool connects.
func Wait(out io.Writer, text string) {
	io.WriteString(out, "\n  "+clean(strings.ReplaceAll(text, "\n", " "))+"\n")
}

// wholeSeconds rounds a time up to whole seconds.
func wholeSeconds(d time.Duration) time.Duration {
	return (d + time.Second - 1) / time.Second * time.Second
}

// Length writes a time in words, such as "30 minutes".
func Length(d time.Duration) string {
	count := func(n time.Duration, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.FormatInt(int64(n), 10) + " " + one + "s"
	}
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return count(d/time.Hour, "hour")
	case d >= time.Minute && d%time.Minute == 0:
		return count(d/time.Minute, "minute")
	case d >= time.Second && d%time.Second == 0:
		return count(d/time.Second, "second")
	}
	return d.String()
}

// Short writes a time in a short form, such as "30 min" or "1 h 30 min".
// Nothing is rounded.
func Short(d time.Duration) string {
	if d < time.Second || d%time.Second != 0 {
		return d.String()
	}
	var parts []string
	for _, unit := range []struct {
		size time.Duration
		name string
	}{{time.Hour, "h"}, {time.Minute, "min"}, {time.Second, "s"}} {
		if n := d / unit.size; n > 0 {
			parts = append(parts, strconv.FormatInt(int64(n), 10)+" "+unit.name)
			d -= n * unit.size
		}
	}
	return strings.Join(parts, " ")
}

// about writes a time that runs, rounded down: to minutes from one minute
// on, to seconds below.
func about(d time.Duration) string {
	switch {
	case d < time.Second:
		return "0 s"
	case d < time.Minute:
		return Short(d / time.Second * time.Second)
	}
	return Short(d / time.Minute * time.Minute)
}
