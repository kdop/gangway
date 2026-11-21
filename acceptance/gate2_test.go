package acceptance

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

const refusedCharacter = "Refused a request with no prompt: it holds a character that is not allowed."

// live fails the test unless the session still takes calls.
func (w *world) live() {
	w.t.Helper()
	got, err := w.call(context.Background(), "POST", "/v1/hello", "")
	if err != nil || got.Status != 200 {
		w.t.Fatalf("the session is not live: %d %s %v", got.Status, got.Body, err)
	}
}

// refusedWithNoPrompt asks for a command that must be refused for a
// character in it, with no prompt and nothing run.
func (w *world) refusedWithNoPrompt(command string) {
	w.t.Helper()
	before := w.prompts()
	w.ask(command).answer().refused(w.t, "control_character")
	w.promptWaits(before)
	w.notRun("ran")
}

func TestGATE10_1_ACarriageReturnIsRefused(t *testing.T) {
	w := newWorld(t)
	w.refusedWithNoPrompt("touch " + w.file("ran") + "\recho hidden")
}

func TestGATE10_2_ControlCharactersAreRefused(t *testing.T) {
	w := newWorld(t)
	for name, bad := range map[string]string{
		"a backspace":   "\b",
		"an Escape":     "\x1b",
		"a colour code": "\x1b[31m",
		"a cursor code": "\x1b[2J",
		"a bell":        "\a",
		"a zero byte":   "\x00",
	} {
		t.Log(name)
		w.refusedWithNoPrompt("touch " + w.file("ran") + " " + bad)
	}
}

func TestGATE10_3_LineBreaksAndTabsAreAllowed(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo one\n\techo two")
	w.prompt(1)
	w.shows("AI asks to run:\n  > echo one\n  > \techo two\n" + question)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "one\ntwo\n" {
		t.Fatalf("want both lines, got %q", got.Output)
	}
}

func TestGATE10_4_TheCodeNeverReachesTheTerminal(t *testing.T) {
	w := newWorld(t)
	w.refusedWithNoPrompt("echo \x1b[2J")
	w.shows(refusedCharacter)
	if strings.Contains(w.screen.raw(), "\x1b") {
		t.Fatal("an Escape reached the terminal")
	}
}

func TestGATE10_6_TheWrittenFormOfACodeIsAllowed(t *testing.T) {
	w := newWorld(t)
	command := `printf 'a\033[31mb\n'`
	w.ask(command)
	w.prompt(1)
	w.shows("AI asks to run:\n  > " + command + "\n" + question)
}

func TestGATE11_1_APipeWorks(t *testing.T) {
	w := newWorld(t)
	a := w.ask(`printf 'b\na\n' | sort`)
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "a\nb\n" {
		t.Fatalf("want the sorted lines, got %q", got.Output)
	}
}

func TestGATE11_2_WritingToAFileWorks(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo kept > " + w.file("out"))
	w.prompt(1)
	w.press("y")
	a.answer().ran(t, 0)
	b, err := os.ReadFile(w.file("out"))
	if err != nil || string(b) != "kept\n" {
		t.Fatalf("want the file to hold %q, got %q, %v", "kept\n", b, err)
	}
}

func TestGATE11_3_OneYesRunsTheWholeLine(t *testing.T) {
	w := newWorld(t)
	command := "touch " + w.file("a") + "; touch " + w.file("b")
	a := w.ask(command)
	w.prompt(1)
	w.shows("  > " + command + "\n" + question)
	w.press("y")
	a.answer().ran(t, 0)
	if !w.exists("a") || !w.exists("b") {
		t.Fatal("not both commands ran")
	}
	w.promptWaits(1)
}

func TestGATE11_4_OneNoRefusesTheWholeLine(t *testing.T) {
	w := newWorld(t)
	a := w.ask("touch " + w.file("a") + "; touch " + w.file("b"))
	w.prompt(1)
	w.press("n")
	a.answer().refused(t, "user")
	w.notRun("a")
	w.notRun("b")
}

func TestGATE11_5_AShellThatIsNotThere(t *testing.T) {
	w := newWorld(t, "COMMAND_SHELL = /no/such/shell")
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	w.press("y")
	got := a.answer()
	if got.Status != 200 || got.State != "ended" || got.How != "start_failed" {
		t.Fatalf("want ended with start_failed, got %d %s", got.Status, got.Body)
	}
	if !strings.Contains(got.Output, "/no/such/shell") {
		t.Fatalf("the answer does not say why: %q", got.Output)
	}
	w.notRun("ran")
}

// hungUp gives a world where the AI hung up on a prompt that waited.
func hungUp(t *testing.T) *world {
	w := newWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(1)
	a.hangUp()
	w.shows("The AI hung up. Nothing ran.")
	return w
}

func TestGATE12_1_HangUpRemovesThePrompt(t *testing.T) {
	w := hungUp(t)
	w.notRun("ran")
	w.live()
}

func TestGATE12_2_AKeyAfterHangUpIsIgnored(t *testing.T) {
	w := hungUp(t)
	w.press("y")
	w.notRun("ran")
	w.live()
}

func TestGATE12_3_TheSameCommandCanBeAskedAgain(t *testing.T) {
	w := hungUp(t)
	a := w.ask("touch " + w.file("ran"))
	w.prompt(2)
	w.press("y")
	a.answer().ran(t, 0)
}

// fullLine gives a world with MAX_WAITING_REQUESTS of 3 and three requests
// that wait.
func fullLine(t *testing.T) (*world, []*asked) {
	w := newWorld(t, "MAX_WAITING_REQUESTS = 3")
	var all []*asked
	for i := 1; i <= 3; i++ {
		all = append(all, w.ask("true"))
		w.waiting(i)
	}
	return w, all
}

func TestGATE13_1_TheLineTakesTheMaximum(t *testing.T) {
	w, all := fullLine(t)
	for _, a := range all {
		a.noAnswerYet()
	}
	w.promptWaits(1)
}

func TestGATE13_2_OneMoreIsRefusedAtOnce(t *testing.T) {
	w, _ := fullLine(t)
	w.ask("touch "+w.file("ran")).answer().refused(t, "line_full")
	w.shows("Refused a request with no prompt: too many requests wait.")
	if strings.Contains(w.screen.text(), w.file("ran")) {
		t.Fatal("the refused request got a prompt")
	}
}

func TestGATE13_3_AnAnswerMakesRoom(t *testing.T) {
	w, all := fullLine(t)
	w.ask("true").answer().refused(t, "line_full")
	// The prompt is drawn again under the line about the refusal.
	w.prompt(2)
	w.press("n")
	all[0].answer().refused(t, "user")
	w.waiting(2)
	more := w.ask("true")
	w.waiting(3)
	more.noAnswerYet()
}

const longShow = 1500 * time.Millisecond

func slowWorld(t *testing.T) *world { return newWorld(t, "PROMPT_MIN_SHOW = "+longShow.String()) }

func TestGATE14_1_AnEarlyYesDoesNotCount(t *testing.T) {
	w := slowWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.promptShown(1)
	w.press("y")
	a.noAnswerYet()
	w.notRun("ran")
}

func TestGATE14_2_ALaterYesCounts(t *testing.T) {
	w := slowWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.promptShown(1)
	w.press("y")
	a.noAnswerYet()
	time.Sleep(longShow)
	w.press("y")
	a.answer().ran(t, 0)
}

func TestGATE14_3_AYesJustAfterHangUpDoesNotAnswerTheNext(t *testing.T) {
	w := slowWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.promptShown(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	time.Sleep(longShow)
	a.hangUp()
	w.promptShown(2)
	w.press("y")
	b.noAnswerYet()
	w.notRun("b")
}

func TestGATE14_4_ASecondNoDoesNotAnswerTheNext(t *testing.T) {
	w := slowWorld(t)
	a := w.ask("touch " + w.file("a"))
	w.promptShown(1)
	b := w.ask("touch " + w.file("b"))
	w.waiting(2)
	time.Sleep(longShow)
	w.press("nn")
	a.answer().refused(t, "user")
	w.promptShown(2)
	b.noAnswerYet()
}

func TestGATE14_5_CtrlCIsNotHeldBack(t *testing.T) {
	w := slowWorld(t)
	a := w.ask("touch " + w.file("ran"))
	w.promptShown(1)
	w.press("\x03")
	a.answer().refused(t, "user")
	w.notRun("ran")
}

// lines counts the lines of a file of the test.
func (w *world) lines(name string) int {
	b, _ := os.ReadFile(w.file(name))
	return strings.Count(string(b), "\n")
}

func TestGATE15_1_OneYesRunsOnce(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo line >> " + w.file("out"))
	w.prompt(1)
	w.press("y")
	a.answer().ran(t, 0)
	w.stays("the file holds one line", func() bool { return w.lines("out") == 1 })
}

func TestGATE15_2_ManyYesRunOnce(t *testing.T) {
	w := newWorld(t)
	a := w.ask("echo line >> " + w.file("out"))
	w.prompt(1)
	for i := 0; i < 10; i++ {
		w.press("y")
		time.Sleep(10 * time.Millisecond)
	}
	a.answer().ran(t, 0)
	w.stays("the file holds one line", func() bool { return w.lines("out") == 1 })
}

func TestGATE15_3_OneYesAnswersOneRequest(t *testing.T) {
	w := newWorld(t)
	command := "echo line >> " + w.file("out")
	a := w.ask(command)
	w.prompt(1)
	b := w.ask(command)
	w.waiting(2)
	w.press("y")
	a.answer().ran(t, 0)
	w.prompt(2)
	b.noAnswerYet()
	w.stays("the file holds one line", func() bool { return w.lines("out") == 1 })
}

const (
	turnAround = "‮" // turns the reading order around
	zeroWidth  = "​" // a space with no width
)

func TestGATE16_1_ATurnedReadingOrderIsRefused(t *testing.T) {
	w := newWorld(t)
	w.refusedWithNoPrompt("touch " + w.file("ran") + " " + turnAround + "txt.exe")
}

func TestGATE16_2_AnInvisibleCharacterIsRefused(t *testing.T) {
	w := newWorld(t)
	w.refusedWithNoPrompt("touch " + w.file("ran") + " na" + zeroWidth + "me")
}

func TestGATE16_3_OtherLettersAreAllowed(t *testing.T) {
	w := newWorld(t)
	command := "echo αβγ café 😀"
	a := w.ask(command)
	w.prompt(1)
	w.shows("  > " + command + "\n" + question)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "αβγ café 😀\n" {
		t.Fatalf("want the letters back, got %q", got.Output)
	}
}

func TestGATE16_4_TheCharacterNeverReachesTheTerminal(t *testing.T) {
	w := newWorld(t)
	w.refusedWithNoPrompt("echo a" + turnAround + "b" + zeroWidth + "c")
	w.shows(refusedCharacter)
	if raw := w.screen.raw(); strings.Contains(raw, turnAround) || strings.Contains(raw, zeroWidth) {
		t.Fatal("the character reached the terminal")
	}
}

func TestGATE17_1_ACommandFindsTheEndOfItsInput(t *testing.T) {
	w := newWorld(t)
	a := w.ask("cat")
	w.prompt(1)
	w.press("y")
	start := time.Now()
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "" || time.Since(start) > 3*time.Second {
		t.Fatalf("want no output at once, got %q after %v", got.Output, time.Since(start))
	}
}

func TestGATE17_2_TypedLettersDoNotReachTheCommand(t *testing.T) {
	w := newWorld(t)
	a := w.ask("sleep 1; cat; echo end")
	w.prompt(1)
	w.press("y")
	time.Sleep(300 * time.Millisecond)
	w.press("abc\r")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "end\n" {
		t.Fatalf("want only %q, got %q", "end\n", got.Output)
	}
}

func TestGATE18_1_PagerAndTermAreSet(t *testing.T) {
	w := newWorld(t)
	a := w.ask(`echo "$PAGER $TERM"`)
	w.prompt(1)
	w.press("y")
	got := a.answer()
	got.ran(t, 0)
	if got.Output != "cat dumb\n" {
		t.Fatalf("want %q, got %q", "cat dumb\n", got.Output)
	}
}
