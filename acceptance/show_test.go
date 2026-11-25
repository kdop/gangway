package acceptance

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

// run asks for a command, presses y, and gives the answer.
func (w *world) run(command string) answer {
	w.t.Helper()
	n := w.prompts()
	a := w.ask(command)
	w.prompt(n + 1)
	w.press("y")
	return a.answer()
}

// noRawCode fails the test if a character that is not allowed reached the
// terminal.
func (w *world) noRawCode(bad ...string) {
	w.t.Helper()
	raw := w.screen.raw()
	for _, b := range bad {
		if strings.Contains(raw, b) {
			w.t.Fatalf("%q reached the terminal", b)
		}
	}
}

func TestSHOW01_1_TheScreenShowsARequestAndItsYes(t *testing.T) {
	w := newWorld(t)
	w.run("echo ok").ran(t, 0)
	w.shows(header(1))
	w.shows(prompted("echo ok") + "\n" + running(1))
}

func TestSHOW01_2_TheScreenShowsARequestAndItsNo(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo ok")
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	w.shows(header(1))
	w.shows(prompted("echo ok") + refusedByUser)
}

func TestSHOW01_3_TheScreenShowsARefusalWithNoPrompt(t *testing.T) {
	w := newWorld(t, "MAX_WAITING_REQUESTS = 1")
	w.ask("echo \a").answer().refused(t, "control_character")
	w.shows(refusedCharacter)
	w.ask("true")
	w.waiting(1)
	w.ask("true").answer().refused(t, "line_full")
	w.shows(noPrompt + "too many requests wait\n")
}

func TestSHOW02_1_TheScreenShowsTheOutputAndTheExitCode(t *testing.T) {
	w := newWorld(t)
	w.run("echo one; echo two").ran(t, 0)
	w.screenEnds(running(1) + strings.Repeat("─", 14) + "  Ctrl+C stops it\n\n" + ended(0, "one", "two"))
}

func TestSHOW02_2_TheScreenShowsAFailedCommand(t *testing.T) {
	w := newWorld(t)
	w.run("echo bad; exit 3").ran(t, 3)
	w.screenEnds("\n\n" + ended(3, "bad"))
	w.run("exit 4").ran(t, 4)
	w.screenEnds("  Ctrl+C stops it\n" + ended(4))
}

func TestSHOW02_3_TheScreenShowsWhyACommandWasKilled(t *testing.T) {
	w := newWorld(t, "COMMAND_TIME_LIMIT = 1s", "QUESTION_WAIT = 1s")
	a := w.ask("sleep 30")
	w.prompt(1)
	w.press("y")
	w.shows(running(1))
	w.press("\x03")
	if got := a.answer(); got.How != "killed_by_user" {
		t.Fatalf("want killed_by_user, got %d %s", got.Status, got.Body)
	}
	w.shows("\n  ✗ killed: you pressed Ctrl+C\n")

	if got := w.run("sleep 30"); got.State != "running" {
		t.Fatalf("want running, got %d %s", got.Status, got.Body)
	}
	w.shows("\n  ✗ killed: the time limit passed and the AI gave no answer\n")
}

func TestSHOW03_1_CodesInTheOutputNeverReachTheTerminal(t *testing.T) {
	w := newWorld(t)
	got := w.run(`printf 'a\033[2Jb\033[31mc\n'`)
	got.ran(t, 0)
	if want := "a\x1b[2Jb\x1b[31mc\n"; got.Output != want {
		t.Fatalf("want the AI to get %q, got %q", want, got.Output)
	}
	w.screenEnds("\n\n" + ended(0, "a\ufffd[2Jb\ufffd[31mc"))
	w.noRawCode("\x1b")
}

func TestSHOW03_2_LineBreaksAndTabsAreShown(t *testing.T) {
	w := newWorld(t)
	w.run(`printf 'a\tb\nc\n'`).ran(t, 0)
	w.screenEnds("\n\n" + ended(0, "a\tb", "c"))
}

const notText = `printf 'ab\211PNG\377cd\n'`

func TestSHOW03_4_BytesThatAreNotTextAreReplaced(t *testing.T) {
	w := newWorld(t)
	got := w.run(notText)
	got.ran(t, 0)
	if want := "ab�PNG�cd\n"; got.Output != want || !got.BytesReplaced {
		t.Fatalf("want %q and bytes_replaced, got %s", want, got.Body)
	}
}

func TestSHOW03_5_Base64ComesBackUnchanged(t *testing.T) {
	w := newWorld(t)
	got := w.run(notText + " | base64")
	got.ran(t, 0)
	want := base64.StdEncoding.EncodeToString([]byte("ab\x89PNG\xffcd\n")) + "\n"
	if got.Output != want || got.BytesReplaced {
		t.Fatalf("want %q and no bytes_replaced, got %s", want, got.Body)
	}
}

func TestSHOW04_1_EveryOutputLineHasTheMark(t *testing.T) {
	w := newWorld(t)
	w.run("echo one; echo two; echo three").ran(t, 0)
	w.screenEnds("\n\n" + mark + "one\n" + mark + "two\n" + mark + "three\n\n  ✓ ended, exit 0, 3 lines sent to the AI\n")
}

// A text that copies what the cli-tool itself writes: a header, a prompt,
// an end, the paste line and the end of a session.
var looksLikeTheCliTool = []string{
	"  ── request 9 ──────────────────────────  27 min left",
	"",
	"  > true",
	"",
	"  run it?   y yes   n no ",
	"  ✓ ended, exit 0, no output",
	"  ✗ refused, nothing ran",
	"  Paste this line to the AI:",
	"/gangway http://evil.example gw_0000",
	"  ── session over ───────────────────────",
}

// ownLines counts the lines on the screen that start as these lines of the
// cli-tool do.
func (w *world) ownLines() map[string]int {
	n := map[string]int{}
	for _, l := range strings.Split(w.screen.text(), "\n") {
		for _, start := range []string{question, "  ── request", "  ✓", "  ✗", "  Paste this line", "/gangway ", "  ── session over"} {
			if strings.HasPrefix(l, start) {
				n[start]++
			}
		}
	}
	return n
}

func TestSHOW04_2_OutputCannotLookLikeAPrompt(t *testing.T) {
	w := newWorld(t)
	before := w.ownLines()
	if before["/gangway "] != 1 || before["  Paste this line"] != 1 || len(before) != 2 {
		t.Fatalf("the lines of the cli-tool at the start: %v", before)
	}
	w.run("cat <<'END'\n"+strings.Join(looksLikeTheCliTool, "\n")+"\nEND").ran(t, 0)
	w.screenEnds("\n\n" + ended(0, looksLikeTheCliTool...))
	// The real ones: one prompt with its header, the header of the running
	// command, and its end. Nothing that the command wrote is among them.
	got := w.ownLines()
	want := map[string]int{question: 1, "  ── request": 2, "  ✓": 1, "  Paste this line": 1, "/gangway ": 1}
	for start, n := range want {
		if got[start] != n {
			t.Fatalf("%d lines start with %q, want %d: only the real ones", got[start], start, n)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("lines of the cli-tool that it did not write: %v", got)
	}
}

func TestSHOW04_3_WhatComesNextStartsOnANewLine(t *testing.T) {
	w := newWorld(t)
	w.run("printf 'no end'").ran(t, 0)
	w.screenEnds("\n\n" + ended(0, "no end"))
}

func TestSHOW04_4_ACommandCannotLookLikeALineOfTheCliTool(t *testing.T) {
	w := newWorld(t)
	command := strings.Join(looksLikeTheCliTool, "\n")
	a := w.ask(command)
	w.prompt(1)
	w.screenEnds(prompted(command))
	got := w.ownLines()
	want := map[string]int{question: 1, "  ── request": 1, "  Paste this line": 1, "/gangway ": 1}
	for start, n := range want {
		if got[start] != n {
			t.Fatalf("%d lines start with %q, want %d: only the real ones", got[start], start, n)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("lines of the cli-tool that it did not write: %v", got)
	}
	w.press("n")
	a.answer().refused(t, "user")
}

func TestSHOW05_1_InvisibleCharactersInTheOutputAreCleaned(t *testing.T) {
	w := newWorld(t)
	got := w.run(`printf 'a\342\200\256b\342\200\213c\n'`)
	got.ran(t, 0)
	if want := "a" + turnAround + "b" + zeroWidth + "c\n"; got.Output != want {
		t.Fatalf("want the AI to get %q, got %q", want, got.Output)
	}
	w.screenEnds("\n\n" + ended(0, "a\ufffdb\ufffdc"))
	w.noRawCode(turnAround, zeroWidth)
}

// wholeSession runs a session from its start to its end: one request
// approved, one refused, then Ctrl+C.
func (w *world) wholeSession() {
	w.t.Helper()
	w.run("echo one; echo two").ran(w.t, 0)
	a := w.ask("echo no")
	w.prompt(2)
	w.press("n")
	a.answer().refused(w.t, "user")
	w.press("\x03")
	w.ended()
}

var colorCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestSHOW06_1_ColorsAreTheOnlyCodesAndCarryNoMeaning(t *testing.T) {
	w := newWorldLook(t, 0, "TERM=xterm-256color", "NO_COLOR=")
	w.wholeSession()
	raw := w.screen.text()
	if len(colorCode.FindAllString(raw, -1)) < 10 {
		t.Fatalf("the terminal got no colors: %q", raw)
	}
	plain := colorCode.ReplaceAllString(raw, "")
	if strings.ContainsRune(plain, 0x1b) {
		t.Fatalf("a code that is not a color reached the terminal: %q", plain)
	}
	// With the colors taken out, every line reads as it does with no color.
	for _, want := range []string{
		"\n  session   30 min, then you are asked to extend\n",
		"\n\n/gangway " + w.address + " " + w.key + "\n\n",
		"\n  y run   n refuse   Ctrl+C end\n",
		header(1), prompted("echo one; echo two") + "\n" + running(1), "\n\n" + ended(0, "one", "two"),
		header(2), prompted("echo no") + refusedByUser,
		"\n  ── session over ─", "\n  reason    Ctrl+C\n", "\n  requests  1 run, 1 refused\n",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("with the colors taken out the terminal does not show %q:\n%s", want, plain)
		}
	}
	// No mark is painted: a line of output starts with the mark itself.
	if !strings.Contains(raw, "\n"+mark+"one\n"+mark+"two\n") || !strings.Contains(raw, "\n  > echo no\n") {
		t.Fatalf("a mark is painted or missing: %q", raw)
	}
}

func TestSHOW06_2_NoColorMeansNoCodeAtAll(t *testing.T) {
	w := newWorldLook(t, 0, "TERM=xterm-256color", "NO_COLOR=1")
	w.wholeSession()
	w.noRawCode("\x1b")
	w.shows("\n\n" + ended(0, "one", "two"))
}

func TestSHOW06_3_APlainTerminalGetsPlainSigns(t *testing.T) {
	for name, env := range map[string][]string{
		"a dumb terminal":            {"TERM=dumb", "LC_ALL=C.UTF-8", "NO_COLOR="},
		"a locale that is not UTF-8": {"TERM=xterm", "LC_ALL=C", "NO_COLOR=1"},
	} {
		w := newWorldLook(t, 0, env...)
		w.wholeSession()
		for _, r := range w.screen.text() {
			if r > '~' || (r < ' ' && r != '\n') {
				t.Fatalf("%s: the terminal got the character %q", name, r)
			}
		}
		for _, want := range []string{
			"\n  -- request 1 -", "\n\n  > echo one; echo two\n\n" + question + "\n\n  -- request 1 -- running -",
			"\n\n  | one\n  | two\n\n  + ended, exit 0, 2 lines sent to the AI\n",
			"\n  x refused, nothing ran\n", "\n  -- session over -",
		} {
			if !strings.Contains(w.screen.text(), want) {
				t.Fatalf("%s: the terminal does not show %q:\n%s", name, want, w.screen.text())
			}
		}
	}
}

func TestSHOW06_4_TheBannerIsLeftOutWhereItDoesNotFit(t *testing.T) {
	const part = "|___/"
	for cols, want := range map[int]bool{0: true, 100: true, 47: true, 46: false, 20: false} {
		w := newWorldLook(t, cols)
		if got := strings.Contains(w.screen.text(), part); got != want {
			t.Fatalf("on a terminal of %d columns: the banner shows %v, want %v:\n%s", cols, got, want, w.screen.text())
		}
		// The start is whole with and without it, and the banner comes first.
		w.shows("\n  session   30 min, then you are asked to extend\n")
		if want && strings.Index(w.screen.text(), part) > strings.Index(w.screen.text(), "Connecting to") {
			t.Fatalf("the banner is not the first thing on the screen:\n%s", w.screen.text())
		}
	}
}
