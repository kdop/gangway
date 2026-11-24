package acceptance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// stepsBuilt holds the steps of docs/plan.md that are built, beside the MVP.
var stepsBuilt = map[string]bool{"2": true, "4": true, "7": true, "11": true}

// builtOfAStepInWork holds the requirements that are built while the rest
// of their step is not. When the whole step is built, the step goes into
// stepsBuilt and its requirements come off this list.
var builtOfAStepInWork = map[string]bool{
	"LEN-08": true, // step 6, built with the session's clock of step 4
}

// noGoTest holds the tests of a built requirement that have no Go test,
// each with the reason.
var noGoTest = map[string]string{
	"GATE-10.5": "file reads are not built",
	"SHOW-03.3": "file reads are not built",
	"LOG-03.2":  "the size cut is not built",
	"CTL-02.2":  "the login of the controller's API is not built",
	"HOST-06.2": "it needs a machine with Tailscale installed and running, and root to check that it was not changed",
}

// TestEveryTestThatIsDueHasAGoTest fails when a Go test here carries an ID
// that docs/requirements.md does not have, and when a test of the MVP or of a
// built step has no Go test.
func TestEveryTestThatIsDueHasAGoTest(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "docs", "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	requirements := string(b)

	inFile := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\| ([A-Z]+-\d+\.\d+) \|`).FindAllStringSubmatch(requirements, -1) {
		inFile[m[1]] = true
	}

	inGo := map[string]bool{}
	files, _ := filepath.Glob("*_test.go")
	for _, f := range files {
		code, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`(?m)^func Test([A-Z]+)(\d+)_(\d+)_`).FindAllStringSubmatch(string(code), -1) {
			id := fmt.Sprintf("%s-%s.%s", m[1], m[2], m[3])
			if inGo[id] {
				t.Errorf("%s has two Go tests", id)
			}
			inGo[id] = true
			if noGoTest[id] != "" {
				t.Errorf("%s has a Go test now: take it off the list noGoTest", id)
			}
			if !inFile[id] {
				t.Errorf("%s has a Go test but is not in docs/requirements.md", id)
			}
		}
	}
	for id := range noGoTest {
		if !inFile[id] {
			t.Errorf("%s is on the list noGoTest but is not in docs/requirements.md", id)
		}
	}

	// The MVP's requirements, from the table under "The MVP".
	due := map[string]bool{}
	for _, row := range regexp.MustCompile(`(?m)^\| ([A-Z]+) \| ([\d, ]+) \|`).FindAllStringSubmatch(requirements, -1) {
		for _, n := range strings.Split(row[2], ",") {
			due[row[1]+"-"+strings.TrimSpace(n)] = true
		}
	}
	if len(due) == 0 {
		t.Fatal("found no MVP table in docs/requirements.md")
	}
	// The requirements of a built step: each says its step at its end.
	ofAStep := 0
	for _, m := range regexp.MustCompile(`(?m)^### ([A-Z]+-\d+)\n\n.* Step (\d+)\.$`).FindAllStringSubmatch(requirements, -1) {
		if stepsBuilt[m[2]] {
			due[m[1]] = true
			ofAStep++
		}
	}
	if ofAStep == 0 {
		t.Fatal("found no requirement of a built step in docs/requirements.md")
	}
	for id := range builtOfAStepInWork {
		if !inFile[id+".1"] {
			t.Errorf("%s is on the list builtOfAStepInWork but is not in docs/requirements.md", id)
		}
		due[id] = true
	}

	var missing []string
	for id := range inFile {
		if due[strings.SplitN(id, ".", 2)[0]] && !inGo[id] && noGoTest[id] == "" {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d tests that are due have no Go test: %s", len(missing), strings.Join(missing, " "))
	}
}
