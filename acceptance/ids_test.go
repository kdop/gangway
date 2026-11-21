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

// notInTheMVP holds the tests of the MVP's requirements that need a
// feature the MVP does not have.
var notInTheMVP = map[string]string{
	"GATE-10.5": "file reads",
	"SHOW-03.3": "file reads",
	"CTL-02.2":  "login",
}

// TestEveryTestOfTheMVPHasAGoTest fails when a Go test here carries an ID
// that requirements.md does not have, and when a test of the MVP has no
// Go test.
func TestEveryTestOfTheMVPHasAGoTest(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "requirements.md"))
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
			if notInTheMVP[id] != "" {
				t.Errorf("%s has a Go test now: take it off the list notInTheMVP", id)
			}
			if !inFile[id] {
				t.Errorf("%s has a Go test but is not in requirements.md", id)
			}
		}
	}

	// The MVP's requirements, from the table under "The MVP".
	mvp := map[string]bool{}
	for _, row := range regexp.MustCompile(`(?m)^\| ([A-Z]+) \| ([\d, ]+) \|`).FindAllStringSubmatch(requirements, -1) {
		for _, n := range strings.Split(row[2], ",") {
			mvp[row[1]+"-"+strings.TrimSpace(n)] = true
		}
	}
	if len(mvp) == 0 {
		t.Fatal("found no MVP table in requirements.md")
	}
	var missing []string
	for id := range inFile {
		if mvp[strings.SplitN(id, ".", 2)[0]] && !inGo[id] && notInTheMVP[id] == "" {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d tests of the MVP have no Go test: %s", len(missing), strings.Join(missing, " "))
	}
}
