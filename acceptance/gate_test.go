package acceptance

import (
	"context"
	"os"
	"os/user"
	"strings"
	"testing"
	"time"
)

func TestGATE01_1_YesRunsTheCommand(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo hello; exit 4")
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 4)
	if got.Output != "hello\n" {
		t.Fatalf("want the output %q, got %q", "hello\n", got.Output)
	}
}

func TestGATE01_2_NoKeyNoRun(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	a.noAnswerYet()
	w.notRun("ran")
	w.promptWaits(1)
}

func TestGATE01_3_LostConnectionNoRun(t *testing.T) {
	w := newWorld(t)
	w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.stopController()
	<-w.cliDone
	w.press("y")
	w.notRun("ran")
}

func TestGATE01_4_EndOfTheCliToolNoRun(t *testing.T) {
	w := newWorld(t)
	w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.cli.Process.Signal(os.Interrupt)
	<-w.cliDone
	w.notRun("ran")
}

func TestGATE02_1_NoRefuses(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	w.notRun("ran")
}

func TestGATE02_2_NoDoesNotAnswerTheNextPrompt(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	b := w.ask("touch " + w.file("ran"))
	w.prompt(2)
	b.noAnswerYet()
	w.notRun("ran")
}

func TestGATE03_1_OtherKeysAreIgnored(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	for _, key := range []string{"\r", " ", "x", "1", "Y", "N", "\x1b"} {
		w.press(key)
		time.Sleep(50 * time.Millisecond)
		if w.exists("ran") {
			t.Fatalf("the command ran after the key %q", key)
		}
	}
	a.noAnswerYet()
	w.notRun("ran")
	w.promptWaits(1)
}

func TestGATE03_2_AWrongKeyDoesNoHarm(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo ok")
	w.prompt(1)
	w.press("x")
	a.noAnswerYet()
	w.press("y")
	a.answer().ran(t, 0)
}

func TestGATE03_3_YesNeedsNoEnter(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo ok")
	w.prompt(1)
	w.press("y")
	a.answer().ran(t, 0)
}

func TestGATE03_4_TheRestOfYesIsIgnored(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("yes")
	a.answer().ran(t, 0)
	w.prompt(2)
	b.noAnswerYet()
	w.notRun("b")
}

func TestGATE03_5_TheRestOfNoIsIgnored(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("no")
	a.answer().refused(t, "user")
	w.prompt(2)
	b.noAnswerYet()
	w.notRun("a")
	w.notRun("b")
}

func TestGATE04_1_AKeyBeforeTheRequestDoesNotCount(t *testing.T) {
	w := newWorld(t)
	w.press("y")
	time.Sleep(100 * time.Millisecond)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	a.noAnswerYet()
	w.notRun("ran")
}

func TestGATE04_2_AKeyWhileACommandRunsDoesNotCount(t *testing.T) {
	w := newWorld(t)
	a := w.ask("sleep 1")
	w.prompt(1)
	w.press("y")
	time.Sleep(300 * time.Millisecond)
	w.press("y")
	a.answer().ran(t, 0)
	b := w.ask("touch " + w.file("ran"))
	w.prompt(2)
	b.noAnswerYet()
	w.notRun("ran")
}

func TestGATE04_3_ASecondYesDoesNotAnswerTheNextPrompt(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo ok")
	w.prompt(1)
	w.press("yy")
	a.answer().ran(t, 0)
	b := w.ask("touch " + w.file("ran"))
	w.prompt(2)
	b.noAnswerYet()
	w.notRun("ran")
}

const quoted = `printf '%s|%s\n' "a  b" 'c "d"' --flag=1`

func TestGATE05_1_ThePromptShowsTheExactRequest(t *testing.T) {
	w := newWorld(t)
	w.ask(quoted)
	w.prompt(1)
	w.shows(header(1))
	w.shows(prompted(quoted))
}

func TestGATE05_2_WhatRunsIsWhatWasShown(t *testing.T) {
	w := newWorld(t)
	a := w.ask(quoted)
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if want := "a  b|c \"d\"\n--flag=1|\n"; got.Output != want {
		t.Fatalf("want the output %q, got %q", want, got.Output)
	}
}

func TestGATE05_3_ALineBreakAndATabAreShownAsTheyAre(t *testing.T) {
	w := newWorld(t)
	w.ask("echo one\n\techo two")
	w.prompt(1)
	w.shows(prompted("echo one\n\techo two"))
}

func TestGATE05_4_EveryLineIsShownBeforeTheQuestion(t *testing.T) {
	w := newWorld(t)
	lines := []string{"echo 1", "echo 2", "echo 3", "echo 4", "echo 5", "echo 6"}
	w.ask(strings.Join(lines, "\n"))
	w.prompt(1)
	w.shows(prompted(strings.Join(lines, "\n")))
}

func TestGATE06_1_OnlyTheFirstRequestIsShown(t *testing.T) {
	w := newWorld(t)
	w.ask("touch " + w.file("a"))
	w.prompt(1)
	w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.promptWaits(1)
	if strings.Contains(w.screen.text(), w.file("b")) {
		t.Fatal("the second request is on the screen")
	}
	w.notRun("b")
}

func TestGATE06_2_TheSecondIsAskedAfterTheFirst(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	w.press("y")
	a.answer().ran(t, 0)
	if !w.exists("a") {
		t.Fatal("the first command has not run")
	}
	w.prompt(2)
	w.shows(header(2))
	w.shows(prompted("touch " + w.file("b")))
	b.noAnswerYet()
	w.notRun("b")
}

func TestGATE06_3_ThreeRequestsThreeAnswersInOrder(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.prompt(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	c := w.ask("touch " + w.file("c"))
	w.waiting(3)
	w.press("n")
	a.answer().refused(t, "user")
	w.prompt(2)
	w.press("y")
	b.answer().ran(t, 0)
	w.prompt(3)
	w.press("n")
	c.answer().refused(t, "user")
	if w.exists("a") || !w.exists("b") || w.exists("c") {
		t.Fatalf("want only b to have run, got a: %v, b: %v, c: %v", w.exists("a"), w.exists("b"), w.exists("c"))
	}
}

func TestGATE07_2_TheAICannotApprove(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	for _, path := range []string{"/v1/approve", "/v1/requests/approve", "/v1/requests/r_1/approve", "/v1/requests/r_1/yes"} {
		got, err := w.call(context.Background(), "POST", path, `{"approved":true}`)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status < 400 {
			t.Fatalf("the call to %s was not refused: %d %s", path, got.Status, got.Body)
		}
	}
	a.noAnswerYet()
	w.notRun("ran")
	w.promptWaits(1)
}

func TestGATE08_1_TheCommandRunsAsTheUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	w := newWorld(t)
	a := w.ask("id -un")
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != me.Username+"\n" {
		t.Fatalf("want the user %q, got %q", me.Username, got.Output)
	}
}
