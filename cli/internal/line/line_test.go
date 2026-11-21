package line

import (
	"fmt"
	"testing"

	"github.com/kdop/gangway/cli/internal/gate"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/shared/designcheck"
)

// The gate takes its requests from the line.
var _ gate.Line = (*Line)(nil)

func req(id string) request.Request {
	return request.Request{ID: id, Command: "echo " + id}
}

// with gives a line that holds these requests. Its limit is high.
func with(ids ...string) *Line {
	l := New(100)
	for _, id := range ids {
		l.Add(req(id), false)
	}
	return l
}

// rest takes out everything with Next and gives the IDs, in order.
func rest(l *Line) string {
	ids := []string{}
	for {
		r, ok := l.Next()
		if !ok {
			return fmt.Sprint(ids)
		}
		ids = append(ids, r.ID)
	}
}

func wantRest(t *testing.T, l *Line, want string) {
	t.Helper()
	if got := rest(l); got != want {
		t.Fatalf("the line holds %s, want %s", got, want)
	}
}

// UNIT-LINE-01. Requirement: GATE-06.
func TestUNITLINE01_OldestFirst(t *testing.T) {
	l := New(100)
	for _, id := range []string{"a", "b", "c"} {
		if !l.Add(req(id), false) {
			t.Fatalf("%s did not fit", id)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		got, ok := l.Next()
		if !ok || got != req(id) {
			t.Fatalf("next gave %v (ok=%v), want %v", got, ok, req(id))
		}
	}
}

// UNIT-LINE-02. Requirement: GATE-06.
func TestUNITLINE02_NextOnAnEmptyLine(t *testing.T) {
	if got, ok := New(100).Next(); ok {
		t.Fatalf("next gave %v from an empty line", got)
	}
}

// UNIT-LINE-03. Requirement: GATE-13.
func TestUNITLINE03_TheLimitWithNoPrompt(t *testing.T) {
	l := New(3)
	for i, want := range []bool{true, true, true, false} {
		if got := l.Add(req(fmt.Sprint(i)), false); got != want {
			t.Fatalf("add number %d gave %v, want %v", i+1, got, want)
		}
	}
}

// UNIT-LINE-04. Requirement: GATE-13.
func TestUNITLINE04_TheLimitWithAPrompt(t *testing.T) {
	l := New(3)
	for i, want := range []bool{true, true, false} {
		if got := l.Add(req(fmt.Sprint(i)), true); got != want {
			t.Fatalf("add number %d gave %v, want %v", i+1, got, want)
		}
	}
}

// UNIT-LINE-05. Requirement: GATE-13.
func TestUNITLINE05_TheRequestAtThePromptStillCounts(t *testing.T) {
	l := New(3)
	for _, id := range []string{"a", "b", "c"} {
		l.Add(req(id), false)
	}
	l.Next()
	if l.Add(req("d"), true) {
		t.Fatal("a request fit with two in the line and one at the prompt, limit 3")
	}
	if !l.Add(req("e"), false) {
		t.Fatal("a request did not fit with two in the line and no prompt, limit 3")
	}
}

// UNIT-LINE-06. Requirement: GATE-13.
func TestUNITLINE06_AFullLineChangesNothing(t *testing.T) {
	l := New(3)
	for _, id := range []string{"a", "b", "c"} {
		l.Add(req(id), false)
	}
	if l.Add(req("d"), false) {
		t.Fatal("a fourth request fit, limit 3")
	}
	wantRest(t, l, "[a b c]")
}

// UNIT-LINE-07. Requirement: GATE-06, GATE-12.
func TestUNITLINE07_RemoveKeepsTheOrder(t *testing.T) {
	l := with("a", "b", "c")
	if !l.Remove("b") {
		t.Fatal("remove did not find b")
	}
	wantRest(t, l, "[a c]")
}

// UNIT-LINE-08. Requirement: GATE-12.
func TestUNITLINE08_RemoveOfAnUnknownID(t *testing.T) {
	l := with("a", "b")
	if l.Remove("zz") {
		t.Fatal("remove found an ID that is not there")
	}
	wantRest(t, l, "[a b]")
}

// UNIT-LINE-09. Requirement: SES-03.
func TestUNITLINE09_Empty(t *testing.T) {
	l := with("a", "b", "c")
	all := l.Empty()
	if len(all) != 3 || all[0] != req("a") || all[1] != req("b") || all[2] != req("c") {
		t.Fatalf("empty gave %v, want a, b, c", all)
	}
	wantRest(t, l, "[]")
}

// UNIT-LINE-10. Requirement: GATE-13.
func TestUNITLINE10_LimitOneWithAPrompt(t *testing.T) {
	if New(1).Add(req("a"), true) {
		t.Fatal("a request fit with one at the prompt, limit 1")
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-LINE", "line_test.go")
}
