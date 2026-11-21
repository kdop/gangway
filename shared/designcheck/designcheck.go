// Package designcheck keeps the unit tests of design.md and the Go tests
// in line. It is used by tests only.
package designcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Match fails the test unless every unit test of design.md whose ID starts
// with prefix, such as "UNIT-GATE", has a Go test in one of testFiles with the same
// requirements in the comment above it, and every requirement named is in
// requirements.md.
func Match(t *testing.T, prefix string, testFiles ...string) {
	t.Helper()
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	root := repoRoot(t)
	design := read(filepath.Join(root, "design.md"))
	requirements := read(filepath.Join(root, "requirements.md"))
	code := ""
	for _, f := range testFiles {
		code += read(f)
	}
	funcPrefix := "Test" + strings.ReplaceAll(prefix, "-", "")

	rows := regexp.MustCompile(`(?m)^\| `+regexp.QuoteMeta(prefix)+`-(\d+) \| ([^|]*) \|`).FindAllStringSubmatch(design, -1)
	if len(rows) == 0 {
		t.Fatalf("found no %s test in design.md", prefix)
	}
	for _, row := range rows {
		num, reqs := row[1], strings.ReplaceAll(row[2], "`", "")
		if reqs == "" {
			t.Errorf("%s-%s names no requirement in design.md", prefix, num)
		}
		for _, id := range strings.Split(reqs, ", ") {
			if !strings.Contains(requirements, "\n### "+id+"\n") {
				t.Errorf("%s-%s names %s, which is not in requirements.md", prefix, num, id)
			}
		}
		comment := "// " + prefix + "-" + num + ". Requirement: " + reqs + "."
		if !strings.Contains(code, comment+"\nfunc "+funcPrefix+num+"_") {
			t.Errorf("%s-%s: no Go test with the comment %q", prefix, num, comment)
		}
	}
	n := len(regexp.MustCompile(`(?m)^func `+funcPrefix+`\d+_`).FindAllString(code, -1))
	if n != len(rows) {
		t.Errorf("%d Go tests carry a %s number, design.md has %d", n, prefix, len(rows))
	}
}

// repoRoot walks up from the folder the test runs in to the folder that
// holds design.md.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "design.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("found no design.md above the test's folder")
		}
		dir = parent
	}
}
