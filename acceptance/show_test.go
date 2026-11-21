package acceptance

import (
	"encoding/base64"
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
	w.shows("AI asks to run:\n  > echo ok\n" + question + "y\n")
}

func TestSHOW01_2_TheScreenShowsARequestAndItsNo(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo ok")
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	w.shows("AI asks to run:\n  > echo ok\n" + question + "n\n")
}

func TestSHOW01_3_TheScreenShowsARefusalWithNoPrompt(t *testing.T) {
	w := newWorld(t, "MAX_WAITING_REQUESTS = 1")
	w.ask("echo \a").answer().refused(t, "control_character")
	w.shows(refusedCharacter)
	w.ask("true")
	w.waiting(1)
	w.ask("true").answer().refused(t, "line_full")
	w.shows("Refused a request with no prompt: too many requests wait.")
}

func TestSHOW02_1_TheScreenShowsTheOutputAndTheExitCode(t *testing.T) {
	w := newWorld(t)
	w.run("echo one; echo two").ran(t, 0)
	w.screenEnds(question + "y\n  | one\n  | two\n")
}

func TestSHOW02_2_TheScreenShowsAFailedCommand(t *testing.T) {
	w := newWorld(t)
	w.run("echo bad; exit 3").ran(t, 3)
	w.screenEnds(question + "y\n  | bad\n")
}

func TestSHOW02_3_TheScreenShowsWhyACommandWasKilled(t *testing.T) {
	w := newWorld(t, "COMMAND_TIME_LIMIT = 1s", "QUESTION_WAIT = 1s")
	a := w.ask("sleep 30")
	w.prompt(1)
	w.press("y")
	w.shows(question + "y\n")
	w.press("\x03")
	if got := a.answer(); got.How != "killed_by_user" {
		t.Fatalf("want killed_by_user, got %d %s", got.Status, got.Body)
	}
	w.shows("Killed: you pressed Ctrl+C.")

	if got := w.run("sleep 30"); got.State != "running" {
		t.Fatalf("want running, got %d %s", got.Status, got.Body)
	}
	w.shows("Killed: the time limit passed and the AI gave no answer.")
}

func TestSHOW03_1_CodesInTheOutputNeverReachTheTerminal(t *testing.T) {
	w := newWorld(t)
	got := w.run(`printf 'a\033[2Jb\033[31mc\n'`)
	got.ran(t, 0)
	if want := "a\x1b[2Jb\x1b[31mc\n"; got.Output != want {
		t.Fatalf("want the AI to get %q, got %q", want, got.Output)
	}
	w.screenEnds("  | a\ufffd[2Jb\ufffd[31mc\n")
	w.noRawCode("\x1b")
}

func TestSHOW03_2_LineBreaksAndTabsAreShown(t *testing.T) {
	w := newWorld(t)
	w.run(`printf 'a\tb\nc\n'`).ran(t, 0)
	w.screenEnds("  | a\tb\n  | c\n")
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
	w.screenEnds("  | one\n  | two\n  | three\n")
}

func TestSHOW04_2_OutputCannotLookLikeAPrompt(t *testing.T) {
	w := newWorld(t)
	w.run(`printf 'AI asks to run:\n  > true\nRun it? y/n '`).ran(t, 0)
	w.screenEnds("  | AI asks to run:\n  |   > true\n  | Run it? y/n \n")
	starts := 0
	for _, l := range strings.Split(w.screen.text(), "\n") {
		if strings.HasPrefix(l, question) || strings.HasPrefix(l, "AI asks to run:") {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("want only the real prompt at the start of a line, got %d such lines", starts)
	}
}

func TestSHOW04_3_WhatComesNextStartsOnANewLine(t *testing.T) {
	w := newWorld(t)
	w.run("printf 'no end'").ran(t, 0)
	w.screenEnds("  | no end\n")
}

func TestSHOW05_1_InvisibleCharactersInTheOutputAreCleaned(t *testing.T) {
	w := newWorld(t)
	got := w.run(`printf 'a\342\200\256b\342\200\213c\n'`)
	got.ran(t, 0)
	if want := "a" + turnAround + "b" + zeroWidth + "c\n"; got.Output != want {
		t.Fatalf("want the AI to get %q, got %q", want, got.Output)
	}
	w.screenEnds("  | a\ufffdb\ufffdc\n")
	w.noRawCode(turnAround, zeroWidth)
}
