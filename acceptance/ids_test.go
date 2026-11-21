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

// TestEveryGoTestIsInTheRequirements fails when a Go test here carries an
// ID that requirements.md does not have. It also lists the tests of the
// MVP that have no Go test yet.
func TestEveryGoTestIsInTheRequirements(t *testing.T) {
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
		if mvp[strings.SplitN(id, ".", 2)[0]] && !inGo[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	t.Logf("%d tests of the MVP have a Go test, %d have none yet: %s", len(inGo), len(missing), strings.Join(missing, " "))
}
