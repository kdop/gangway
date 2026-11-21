package checks

import (
	"fmt"
	"testing"

	"github.com/kdop/gangway/cli/internal/designcheck"
	"github.com/kdop/gangway/cli/internal/request"
)

func wantPass(t *testing.T, command string) {
	t.Helper()
	if reason, ok := Check(request.Request{ID: "a", Command: command}); !ok || reason != "" {
		t.Errorf("%q was refused with %q, want it to pass", command, reason)
	}
}

func wantRefused(t *testing.T, command string) {
	t.Helper()
	reason, ok := Check(request.Request{ID: "a", Command: command})
	if ok || reason != ReasonControlCharacter {
		t.Errorf("%q gave ok=%v reason=%q, want a refusal with %q", command, ok, reason, ReasonControlCharacter)
	}
}

// UNIT-CHECKS-01. Requirement: GATE-10.
func TestUNITCHECKS01_APlainCommandPasses(t *testing.T) {
	wantPass(t, "grep ERROR app.log | tail -n 20 > /tmp/out.txt 2>&1")
}

// UNIT-CHECKS-02. Requirement: GATE-10.
func TestUNITCHECKS02_LineBreaksAndTabsPass(t *testing.T) {
	wantPass(t, "cat > notes.txt <<'END'\n\tfirst line\n\tsecond line\nEND")
}

// UNIT-CHECKS-03. Requirement: GATE-10.
func TestUNITCHECKS03_TheFirstSetIsRefused(t *testing.T) {
	for code := 0; code < 32; code++ {
		if code == '\n' || code == '\t' {
			continue
		}
		wantRefused(t, fmt.Sprintf("echo a%cb", rune(code)))
	}
}

// UNIT-CHECKS-04. Requirement: GATE-10.
func TestUNITCHECKS04_DeleteIsRefused(t *testing.T) {
	wantRefused(t, "echo a\x7fb")
}

// UNIT-CHECKS-05. Requirement: GATE-10.
func TestUNITCHECKS05_TheSecondSetIsRefused(t *testing.T) {
	for code := 128; code < 160; code++ {
		wantRefused(t, fmt.Sprintf("echo a%cb", rune(code)))
	}
}

// UNIT-CHECKS-06. Requirement: GATE-10.
func TestUNITCHECKS06_WrittenFormsPass(t *testing.T) {
	for _, c := range []string{`printf '\033[31mred'`, `printf 'a\nb'`, `printf 'a\rb'`, `printf '\x1b[0m'`} {
		wantPass(t, c)
	}
}

// UNIT-CHECKS-07. Requirement: GATE-10.
func TestUNITCHECKS07_BytesThatAreNotTextAreRefused(t *testing.T) {
	for _, c := range []string{"echo \xff", "echo \x9b31m", "echo \xc3", "echo \xed\xa0\x80"} {
		wantRefused(t, c)
	}
}

// UNIT-CHECKS-08. Requirement: GATE-10.
func TestUNITCHECKS08_OtherLanguagesPass(t *testing.T) {
	for _, c := range []string{"echo καλημέρα", "echo café", "echo 👍", "echo a b", "echo 日本語"} {
		wantPass(t, c)
	}
}

// UNIT-CHECKS-09. Requirement: GATE-10.
func TestUNITCHECKS09_TheWholeTextIsRead(t *testing.T) {
	for _, c := range []string{"\x1b[2Jls", "ls \x1b[2J -l", "ls -l\x1b"} {
		wantRefused(t, c)
	}
}

// UNIT-CHECKS-10. Requirement: GATE-10.
func TestUNITCHECKS10_CarriageReturnWithALineBreakIsRefused(t *testing.T) {
	wantRefused(t, "echo a\r\necho b")
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-CHECKS", "checks_test.go")
}
