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
// that docs/requirements.md does not have, and when a test of a built
// requirement has no Go test.
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

	// The versions that are built or released, from the table in
	// docs/releases.md.
	r, err := os.ReadFile(filepath.Join("..", "docs", "releases.md"))
	if err != nil {
		t.Fatal(err)
	}
	built := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\| (\d+\.\d+) \| (Built|Released) \|`).FindAllStringSubmatch(string(r), -1) {
		built[m[1]] = true
	}
	if len(built) == 0 {
		t.Fatal("found no built version in docs/releases.md")
	}

	// Each requirement ends with its version, with "No version yet" or
	// with "Dropped".
	due := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^### ([A-Z]+-\d+)\n\n(.*)$`).FindAllStringSubmatch(requirements, -1) {
		end := regexp.MustCompile(` (?:Version (\d+\.\d+)\.|No version yet\.|Dropped: .*\.)$`).FindStringSubmatch(m[2])
		if end == nil {
			t.Errorf("%s does not end with its version, with \"No version yet.\" or with \"Dropped: …\"", m[1])
			continue
		}
		if built[end[1]] {
			due[m[1]] = true
		}
	}
	if len(due) == 0 {
		t.Fatal("found no requirement of a built version in docs/requirements.md")
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
