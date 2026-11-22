package start

import (
	"strings"
	"testing"

	"github.com/kdop/gangway/shared/designcheck"
)

// UNIT-START-01. Requirement: HOST-01.
func TestUNITSTART01_RootIsRefused(t *testing.T) {
	err := Check(0, true, true)
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("the error is %v, want one about root", err)
	}
}

// UNIT-START-02. Requirement: HOST-02.
func TestUNITSTART02_NoTerminalIsRefused(t *testing.T) {
	for _, c := range [][2]bool{{false, true}, {true, false}, {false, false}} {
		err := Check(1000, c[0], c[1])
		if err == nil || !strings.Contains(err.Error(), "terminal") {
			t.Fatalf("input %v, output %v: the error is %v, want one about a terminal", c[0], c[1], err)
		}
	}
}

// UNIT-START-03. Requirement: HOST-01, HOST-02.
func TestUNITSTART03_APlainUserAtATerminal(t *testing.T) {
	if err := Check(1000, true, true); err != nil {
		t.Fatal(err)
	}
}

// UNIT-START-04. Requirement: HOST-01.
func TestUNITSTART04_RootComesFirst(t *testing.T) {
	err := Check(0, false, false)
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("the error is %v, want the one about root", err)
	}
}

// UNIT-START-05. Requirement: HOST-04, HOST-08.
func TestUNITSTART05_TheWordsAfterTheName(t *testing.T) {
	for _, c := range []struct {
		args    []string
		what    int
		address string
	}{
		{nil, doSession, ""},
		{[]string{"login"}, doLogin, ""},
		{[]string{"--local"}, doLocal, ""},
		{[]string{"--local", "http://127.0.0.1:9000"}, doLocal, "http://127.0.0.1:9000"},
	} {
		what, address, err := Plan(c.args)
		if err != nil || what != c.what || address != c.address {
			t.Fatalf("%v gave %d, %q, %v", c.args, what, address, err)
		}
	}
}

// UNIT-START-06. Requirement: HOST-04.
func TestUNITSTART06_WordsThatMakeNoSense(t *testing.T) {
	for _, args := range [][]string{
		{"start"}, {"login", "now"}, {"-local"}, {"--local", "a", "b"}, {"--help"}, {"Login"}, {""}, {"login", "--local"},
	} {
		_, _, err := Plan(args)
		if err == nil || !strings.Contains(err.Error(), "gangway login") || !strings.Contains(err.Error(), "gangway --local") {
			t.Fatalf("%v gave %v, want the text on how to start", args, err)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-START", "start_test.go")
}
