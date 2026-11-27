package acceptance

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests run the real install.sh. A web server of the test stands in
// for GitHub, and a small shell script stands in for the cli-tool. The
// programs `id` and `uname` are stand-ins too, so that a test picks the
// user and the kind of machine.

const (
	installAmd64    = "gangway-linux-amd64"
	installArm64    = "gangway-linux-arm64"
	controllerAmd64 = "gangway-controller-linux-amd64"
	controllerArm64 = "gangway-controller-linux-arm64"
)

// standInTool is what a release holds in the place of the cli-tool.
func standInTool(what string) []byte {
	return []byte("#!/bin/sh\necho 'stand-in for gangway: " + what + "'\n")
}

// sums writes a SHA256SUMS for the files, the way sha256sum does.
func sums(files map[string][]byte) []byte {
	var b bytes.Buffer
	for _, name := range []string{installAmd64, installArm64, controllerAmd64, controllerArm64} {
		if content, ok := files[name]; ok {
			fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(content), name)
		}
	}
	return b.Bytes()
}

// releases is the web server that stands in for the releases of GitHub.
type releases struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	asked int
}

func newReleases(t *testing.T) *releases {
	t.Helper()
	r := &releases{files: map[string][]byte{}}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.asked++
		content, ok := r.files[req.URL.Path]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(content)
	}))
	t.Cleanup(r.Close)
	return r
}

// release puts a release on the server, with a SHA256SUMS that matches.
// The version "" is the latest release.
func (r *releases) release(version string, files map[string][]byte) {
	at := "/latest/download/"
	if version != "" {
		at = "/download/" + version + "/"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, content := range files {
		r.files[at+name] = content
	}
	r.files[at+"SHA256SUMS"] = sums(files)
}

func (r *releases) set(path string, content []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[path] = content
}

func (r *releases) drop(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, path)
}

func (r *releases) downloads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked
}

// usual puts a latest release and v0.1.0 on the server, each with its own
// files for both kinds of machine.
func (r *releases) usual() {
	r.release("", map[string][]byte{
		installAmd64:    standInTool("latest amd64"),
		controllerAmd64: standInTool("latest controller amd64"),
		controllerArm64: standInTool("latest controller arm64"),
		installArm64:    standInTool("latest arm64"),
	})
	r.release("v0.1.0", map[string][]byte{
		installAmd64:    standInTool("v0.1.0 amd64"),
		controllerAmd64: standInTool("v0.1.0 controller amd64"),
		controllerArm64: standInTool("v0.1.0 controller arm64"),
		installArm64:    standInTool("v0.1.0 arm64"),
	})
}

// installHost is one host the install runs on.
type installHost struct {
	t       *testing.T
	home    string
	temp    string
	tools   string
	from    string
	onPath  bool
	script  string
	uid     string
	system  string
	machine string
}

func newInstallHost(t *testing.T, r *releases) *installHost {
	t.Helper()
	dir := t.TempDir()
	h := &installHost{
		t:       t,
		home:    filepath.Join(dir, "home"),
		temp:    filepath.Join(dir, "temp"),
		tools:   filepath.Join(dir, "tools"),
		script:  filepath.Join("..", "install.sh"),
		uid:     "1000",
		system:  "Linux",
		machine: "x86_64",
	}
	if r != nil {
		h.from = r.URL
	}
	for _, folder := range []string{h.home, h.temp, h.tools} {
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h *installHost) target() string {
	return filepath.Join(h.home, ".local", "bin", "gangway")
}

func (h *installHost) write(path string, content []byte, mode os.FileMode) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		h.t.Fatal(err)
	}
}

// run runs the install script with the given version, or none, and gives
// what it wrote to the screen and whether it ended well.
func (h *installHost) run(args ...string) (string, bool) {
	h.t.Helper()
	h.write(filepath.Join(h.tools, "id"), []byte("#!/bin/sh\necho "+h.uid+"\n"), 0o755)
	h.write(filepath.Join(h.tools, "uname"), []byte("#!/bin/sh\ncase \"$1\" in\n-s) echo "+h.system+" ;;\n-m) echo "+h.machine+" ;;\n*) exit 1 ;;\nesac\n"), 0o755)

	path := h.tools + string(os.PathListSeparator) + os.Getenv("PATH")
	if h.onPath {
		path = filepath.Dir(h.target()) + string(os.PathListSeparator) + path
	}
	cmd := exec.Command("sh", append([]string{h.script}, args...)...)
	cmd.Env = []string{"HOME=" + h.home, "PATH=" + path, "TMPDIR=" + h.temp}
	if h.from != "" {
		cmd.Env = append(cmd.Env, "GANGWAY_INSTALL_FROM="+h.from)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// installs fails the test unless the install ends well.
func (h *installHost) installs(args ...string) string {
	h.t.Helper()
	out, ok := h.run(args...)
	if !ok {
		h.t.Fatalf("the install failed:\n%s", out)
	}
	return out
}

// stops fails the test unless the install stops and says want.
func (h *installHost) stops(want string, args ...string) string {
	h.t.Helper()
	out, ok := h.run(args...)
	if ok {
		h.t.Fatalf("the install ended well, want it to stop:\n%s", out)
	}
	if !strings.Contains(out, want) {
		h.t.Fatalf("the install stopped without saying %q:\n%s", want, out)
	}
	return out
}

// holds fails the test unless the installed cli-tool is want, byte for
// byte, with the rights of a program.
func (h *installHost) holds(want []byte) {
	h.t.Helper()
	got, err := os.ReadFile(h.target())
	if err != nil {
		h.t.Fatalf("the cli-tool is not installed: %v", err)
	}
	if !bytes.Equal(got, want) {
		h.t.Fatalf("the installed cli-tool holds %q, want %q", got, want)
	}
	info, err := os.Stat(h.target())
	if err != nil {
		h.t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		h.t.Fatalf("the installed cli-tool has the rights %o, want 755", info.Mode().Perm())
	}
}

// runsAs fails the test unless the installed cli-tool can be run and
// prints want.
func (h *installHost) runsAs(want string) {
	h.t.Helper()
	out, err := exec.Command(h.target()).CombinedOutput()
	if err != nil {
		h.t.Fatalf("the installed cli-tool cannot be run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), want) {
		h.t.Fatalf("the installed cli-tool printed %q, want %q", out, want)
	}
}

// filesUnder lists every file and folder under a folder, each with its rights
// and what it holds.
func filesUnder(t *testing.T, root string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			found[rel] = fmt.Sprintf("folder %o", info.Mode().Perm())
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found[rel] = fmt.Sprintf("file %o %x", info.Mode().Perm(), sha256.Sum256(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// unchanged fails the test unless the home folder is as it was at before,
// but for the paths in allowed, and the temp folder is empty.
func (h *installHost) unchanged(before map[string]string, allowed ...string) {
	h.t.Helper()
	skip := map[string]bool{}
	for _, path := range allowed {
		skip[path] = true
	}
	after := filesUnder(h.t, h.home)
	for path, was := range before {
		if skip[path] {
			continue
		}
		if now, ok := after[path]; !ok {
			h.t.Errorf("%s is gone", path)
		} else if now != was {
			h.t.Errorf("%s was changed: it was %s and is %s", path, was, now)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok && !skip[path] {
			h.t.Errorf("%s was left behind", path)
		}
	}
	if left := filesUnder(h.t, h.temp); len(left) != 0 {
		h.t.Errorf("the temp folder holds %v, want nothing", left)
	}
}

// installedFolders are the folders the install may make on its way to the
// cli-tool.
var installedFolders = []string{".local", filepath.Join(".local", "bin")}

func TestINS01_1_TheInstallPutsTheCliToolInTheHomeFolder(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)

	out := h.installs()
	h.holds(standInTool("latest amd64"))
	h.runsAs("latest amd64")
	for _, want := range []string{h.target(), "gangway login"} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen does not show %q:\n%s", want, out)
		}
	}
}

func TestINS01_2_AnAarch64MachineGetsItsOwnFile(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.machine = "aarch64"

	h.installs()
	h.holds(standInTool("latest arm64"))
}

func TestINS01_3_AVersionCanBeAskedFor(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)

	h.installs("v0.1.0")
	h.holds(standInTool("v0.1.0 amd64"))
}

func TestINS02_1_AWrongChecksumInstallsNothing(t *testing.T) {
	r := newReleases(t)
	r.usual()
	r.set("/latest/download/"+installAmd64, standInTool("not the file of the release"))
	h := newInstallHost(t, r)
	before := filesUnder(t, h.home)

	h.stops("does not match its checksum")
	if _, err := os.Stat(h.target()); !os.IsNotExist(err) {
		t.Errorf("the cli-tool was installed, or cannot be looked at: %v", err)
	}
	h.unchanged(before, installedFolders...)
}

func TestINS02_2_AWrongChecksumKeepsTheOldCliTool(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.installs("v0.1.0")
	before := filesUnder(t, h.home)
	r.set("/latest/download/"+installAmd64, standInTool("not the file of the release"))

	h.stops("does not match its checksum")
	h.holds(standInTool("v0.1.0 amd64"))
	h.unchanged(before)
}

func TestINS02_3_NoReleaseInstallsNothing(t *testing.T) {
	r := newReleases(t)
	h := newInstallHost(t, r)
	before := filesUnder(t, h.home)

	h.stops("There may be no release yet")
	h.unchanged(before, installedFolders...)

	// A release with no file for this kind of machine.
	r.release("", map[string][]byte{installArm64: standInTool("latest arm64")})
	h.stops("There may be no release yet")
	h.unchanged(before, installedFolders...)
}

func TestINS02_4_NoChecksumFileInstallsNothing(t *testing.T) {
	r := newReleases(t)
	r.usual()
	r.drop("/latest/download/SHA256SUMS")
	h := newInstallHost(t, r)
	before := filesUnder(t, h.home)

	h.stops("cannot be checked")
	h.unchanged(before, installedFolders...)
}

func TestINS02_5_NoLineInTheChecksumFileInstallsNothing(t *testing.T) {
	r := newReleases(t)
	r.usual()
	r.set("/latest/download/SHA256SUMS", sums(map[string][]byte{installArm64: standInTool("latest arm64")}))
	h := newInstallHost(t, r)
	before := filesUnder(t, h.home)

	h.stops("cannot be checked")
	h.unchanged(before, installedFolders...)
}

func TestINS03_1_ASecondInstallReplacesTheCliTool(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.installs("v0.1.0")
	h.holds(standInTool("v0.1.0 amd64"))

	h.installs()
	h.holds(standInTool("latest amd64"))
	h.runsAs("latest amd64")
}

func TestINS03_2_ASecondInstallCanGoBackToAnOlderVersion(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.installs()

	h.installs("v0.1.0")
	h.holds(standInTool("v0.1.0 amd64"))
}

// refusesAtOnce fails the test unless the install stops with want, with
// nothing downloaded and nothing written.
func refusesAtOnce(t *testing.T, r *releases, h *installHost, want string, args ...string) {
	t.Helper()
	before := filesUnder(t, h.home)
	h.stops(want, args...)
	if n := r.downloads(); n != 0 {
		t.Errorf("%d downloads were made, want none", n)
	}
	h.unchanged(before)
}

func TestINS04_1_RootIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.uid = "0"

	refusesAtOnce(t, r, h, "do not run this as root")
}

func TestINS04_2_ASystemThatIsNotLinuxIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.system = "Darwin"

	refusesAtOnce(t, r, h, "gangway runs on Linux only")
}

func TestINS04_3_AnUnknownKindOfMachineIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.machine = "riscv64"

	refusesAtOnce(t, r, h, "riscv64")
}

func TestINS05_1_NothingButTheCliToolIsWritten(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.write(filepath.Join(h.home, ".profile"), []byte("# the user's own\n"), 0o644)
	h.write(filepath.Join(h.home, ".bashrc"), []byte("# the user's own\n"), 0o644)
	h.write(filepath.Join(h.home, ".config", "gangway", "tailscale-auth-key"), []byte("a stand-in for the key\n"), 0o600)
	h.write(filepath.Join(h.home, ".config", "gangway", "settings"), []byte("SESSION_LENGTH = 45m\n"), 0o600)
	before := filesUnder(t, h.home)

	h.installs()
	h.holds(standInTool("latest amd64"))
	h.unchanged(before, append([]string{filepath.Join(".local", "bin", "gangway")}, installedFolders...)...)
}

func TestINS05_2_AMissingPathIsSaidAndNotFixed(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.write(filepath.Join(h.home, ".profile"), []byte("# the user's own\n"), 0o644)
	h.write(filepath.Join(h.home, ".bashrc"), []byte("# the user's own\n"), 0o644)
	before := filesUnder(t, h.home)

	out := h.installs()
	for _, want := range []string{"is not on your PATH", `export PATH="$HOME/.local/bin:$PATH"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen does not show %q:\n%s", want, out)
		}
	}
	h.unchanged(before, append([]string{filepath.Join(".local", "bin", "gangway")}, installedFolders...)...)
}

func TestINS05_3_NothingIsSaidWhenThePathIsRight(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.onPath = true

	out := h.installs()
	if strings.Contains(out, "PATH") {
		t.Errorf("the screen speaks of PATH:\n%s", out)
	}
}

func TestINS06_1_APlainHttpAddressOnAnotherMachineIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.from = "http://releases.example.invalid/gangway"

	refusesAtOnce(t, r, h, "must start with https://")
}

func TestINS06_2_AnAddressWithAnAtIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	// A browser or curl reads this as the machine after the @.
	h.from = "http://127.0.0.1:80@" + strings.TrimPrefix(r.URL, "http://")

	refusesAtOnce(t, r, h, "must not hold an @")
}

func TestINS06_3_AVersionThatIsNotAVersionIsRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)

	refusesAtOnce(t, r, h, "is not a version", "../x")
}

func TestINS07_1_ACutScriptRunsNothing(t *testing.T) {
	r := newReleases(t)
	r.usual()
	whole, err := os.ReadFile(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	whole = bytes.TrimRight(whole, "\n")

	// After every line, and after every character of the end, where the
	// script is started.
	cuts := map[int]bool{}
	for i, c := range whole {
		if c == '\n' {
			cuts[i+1] = true
		}
	}
	for i := len(whole) - 200; i < len(whole); i++ {
		if i > 0 {
			cuts[i] = true
		}
	}
	if len(cuts) < 100 {
		t.Fatalf("only %d cuts of the script were found", len(cuts))
	}

	h := newInstallHost(t, r)
	h.script = filepath.Join(t.TempDir(), "install.sh")
	for cut := range cuts {
		if err := os.WriteFile(h.script, whole[:cut], 0o600); err != nil {
			t.Fatal(err)
		}
		out, _ := h.run()
		if n := r.downloads(); n != 0 {
			t.Fatalf("the script cut after %d of %d characters made %d downloads:\n%s", cut, len(whole), n, out)
		}
		if left := filesUnder(t, h.home); len(left) != 0 {
			t.Fatalf("the script cut after %d of %d characters wrote %v:\n%s", cut, len(whole), left, out)
		}
	}

	// The whole script, run the same way, does install: the cuts above
	// proved something.
	if err := os.WriteFile(h.script, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	h.installs()
	h.holds(standInTool("latest amd64"))
}

// controllerTarget is where the install puts the controller.
func (h *installHost) controllerTarget() string {
	return filepath.Join(h.home, ".local", "bin", "gangway-controller")
}

// holdsAt fails the test unless the file at path is want, byte for byte,
// with the rights of a program, and prints said when it is run.
func (h *installHost) holdsAt(path string, want []byte, said string) {
	h.t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("%s is not installed: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		h.t.Fatalf("%s holds %q, want %q", path, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		h.t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		h.t.Fatalf("%s has the rights %o, want 755", path, info.Mode().Perm())
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil || !strings.Contains(string(out), said) {
		h.t.Fatalf("%s printed %q, %v, want %q", path, out, err, said)
	}
}

// absent fails the test unless there is no file at path.
func (h *installHost) absent(path string) {
	h.t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		h.t.Errorf("%s is there, or cannot be looked at: %v", path, err)
	}
}

func TestINS08_1_TheWordControllerInstallsTheController(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)

	out := h.installs("controller")
	h.holdsAt(h.controllerTarget(), standInTool("latest controller amd64"), "latest controller amd64")
	h.absent(h.target())
	for _, want := range []string{h.controllerTarget(), "gangway-controller login"} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen does not show %q:\n%s", want, out)
		}
	}
}

func TestINS08_2_TheWordAllInstallsBoth(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	h.machine = "aarch64"

	out := h.installs("all")
	h.holdsAt(h.target(), standInTool("latest arm64"), "latest arm64")
	h.holdsAt(h.controllerTarget(), standInTool("latest controller arm64"), "latest controller arm64")
	for _, want := range []string{h.target(), h.controllerTarget(), "gangway-controller -local", "gangway --local"} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen does not show %q:\n%s", want, out)
		}
	}
}

func TestINS08_3_WithAllAWrongChecksumInstallsNeither(t *testing.T) {
	r := newReleases(t)
	r.usual()
	r.set("/latest/download/"+controllerAmd64, standInTool("not the file of the release"))
	h := newInstallHost(t, r)
	before := filesUnder(t, h.home)

	h.stops("does not match its checksum", "all")
	h.absent(h.target())
	h.absent(h.controllerTarget())
	h.unchanged(before, installedFolders...)
}

func TestINS08_4_AWordAndAVersionGoTogetherInAnyOrder(t *testing.T) {
	for _, args := range [][]string{{"controller", "v0.1.0"}, {"v0.1.0", "controller"}} {
		r := newReleases(t)
		r.usual()
		h := newInstallHost(t, r)

		h.installs(args...)
		h.holdsAt(h.controllerTarget(), standInTool("v0.1.0 controller amd64"), "v0.1.0 controller amd64")
		h.absent(h.target())
	}
}

func TestINS08_5_TwoWordsAreRefused(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)
	refusesAtOnce(t, r, h, "say once what to install", "controller", "all")
}

func TestINS08_6_TheWordCliInstallsTheCliTool(t *testing.T) {
	r := newReleases(t)
	r.usual()
	h := newInstallHost(t, r)

	h.installs("cli")
	h.holds(standInTool("latest amd64"))
	h.absent(h.controllerTarget())
}
