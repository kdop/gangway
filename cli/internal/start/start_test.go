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

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-START", "start_test.go")
}
