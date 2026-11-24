package acceptance

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"tailscale.com/tailcfg"
)

// The row also says "The UDP ports are the Tailscale tunnel's". The test
// checks that through what the host told Tailscale: each UDP port it
// holds on an address family it announced is one of the announced ports.
// Not checked: the IPv6 port on a machine with no IPv6 address, since
// the host then announces no IPv6 port to compare with.
func TestHOST03_1_NoPortOfTheCliToolAnswersFromAnotherNetwork(t *testing.T) {
	w, control := newTailscaleWorld(t)
	w.run("echo live").ran(t, 0)

	address, reaches := otherNetwork(t)
	// A server that does answer everywhere shows that the cable works.
	open, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	go func() {
		for {
			c, err := open.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(open.Addr().String())
	if !reaches(port) {
		t.Fatalf("the test is of no use: the second network cannot reach %s at all", address)
	}

	tcp, _ := portsOf(t, w.cli.Process.Pid)
	for _, held := range tcp {
		if reaches(strconv.Itoa(held.port)) {
			t.Errorf("the cli-tool answers a call from another network on the TCP port %d", held.port)
		}
	}

	// The library tries a few ports at the start and lets them go.
	why := ""
	tunnelOnly := func() bool {
		_, udp := portsOf(t, w.cli.Process.Pid)
		hosts := control.hosts()
		if len(hosts) != 1 {
			why = "the stand-in network does not have one host"
			return false
		}
		announced := map[string]map[int]bool{"udp": {}, "udp6": {}}
		for _, e := range hosts[0].Endpoints {
			kind := "udp6"
			if e.Addr().Is4() {
				kind = "udp"
			}
			announced[kind][int(e.Port())] = true
		}
		perKind := map[string]int{}
		for _, held := range udp {
			perKind[held.kind]++
			if perKind[held.kind] > 1 {
				why = "it holds more than one UDP port of the kind " + held.kind
				return false
			}
			if len(announced[held.kind]) > 0 && !announced[held.kind][held.port] {
				why = "it holds the UDP port " + strconv.Itoa(held.port) + ", which it did not announce to Tailscale"
				return false
			}
		}
		if perKind["udp"] != 1 || len(announced["udp"]) == 0 {
			why = "it holds no IPv4 UDP port that it announced to Tailscale"
			return false
		}
		return true
	}
	for end := time.Now().Add(soon); !tunnelOnly(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("the UDP ports of the cli-tool are not the tunnel's only: %s", why)
		}
	}
}

func TestHOST03_2_InTheOneMachineModeTheCliToolHoldsNoPort(t *testing.T) {
	w := newWorld(t)
	w.run("echo live").ran(t, 0)
	w.stays("the cli-tool holds no port", func() bool {
		tcp, udp := portsOf(t, w.cli.Process.Pid)
		return len(tcp) == 0 && len(udp) == 0
	})
}

// loginWorld gives a controller on the stand-in network and a host where
// "gangway login" has been done, as the user would do it.
func loginWorld(t *testing.T) (*world, *tailnet) {
	t.Helper()
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, "")
	w.startTailscaleController()
	w.cliLogin(tailscaleKey).loggedIn(tailscaleKey, cliReady)
	return w, control
}

// nothingAsked fails the test if the cli-tool asked for the auth key.
func (w *world) nothingAsked() {
	w.t.Helper()
	if strings.Contains(w.screen.text(), "Paste the Tailscale auth key") {
		w.t.Fatal("the cli-tool asked for the auth key at a start")
	}
}

func TestHOST04_1_AfterLoginAStartAsksNothing(t *testing.T) {
	w, _ := loginWorld(t)
	// startCli types nothing and waits for the paste line.
	w.startCli(nil)
	w.nothingAsked()
	w.run("echo first").ran(t, 0)
}

func TestHOST04_2_ASecondStartAsksNothing(t *testing.T) {
	w, _ := loginWorld(t)
	w.startCli(nil)
	w.run("echo first").ran(t, 0)
	first := w.key
	w.endByCtrlC()

	w.startCli(nil)
	w.nothingAsked()
	if w.key == first {
		t.Fatal("the second start shows the key of the first session")
	}
	w.run("echo second").ran(t, 0)
}

// ownerOnly fails the test unless the file is the user's own and no
// other user can read or write it.
func ownerOnly(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s has the rights %o, want %o", path, info.Mode().Perm(), want)
	}
	if owner := info.Sys().(*syscall.Stat_t).Uid; int(owner) != os.Getuid() {
		t.Errorf("%s belongs to the user %d, not to the user of the test", path, owner)
	}
}

// login runs with a mask that leaves new files open to all: see
// onTerminal.
func TestHOST05_1_OnlyTheOwnerCanReadTheAuthKeyFile(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, "", "")
	w.cliLogin(tailscaleKey).loggedIn(tailscaleKey, cliReady)
	b, err := os.ReadFile(w.cliKeyFile())
	if err != nil || strings.TrimSpace(string(b)) != tailscaleKey {
		t.Fatalf("the auth key file does not hold the key: %v", err)
	}
	ownerOnly(t, w.cliKeyFile(), 0o600)
}

func TestHOST06_3_WithNoLoginTheCliToolSaysToRunLogin(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, "", "")

	w.noPasteLine = true
	w.startCli(nil)
	w.ended()
	if code := w.cli.ProcessState.ExitCode(); code == 0 {
		t.Fatal("the cli-tool ended with exit code 0")
	}
	w.shows(`run "gangway login" first`)
	w.stays("no call reached Tailscale", func() bool { return control.calls.Load() == 0 })
}

// The stand-in network takes the key at login and refuses it afterwards,
// as Tailscale does with a key that ran out or was taken back.
func TestHOST06_4_AKeyThatTailscaleNoLongerTakes(t *testing.T) {
	w, control := loginWorld(t)
	control.RequireAuthKey = otherTailscaleKey
	before := len(control.hosts()) // the device that login tried the key with

	w.noPasteLine = true
	w.startCli(nil)
	w.ended()
	if code := w.cli.ProcessState.ExitCode(); code == 0 {
		t.Fatal("the cli-tool ended with exit code 0")
	}
	w.shows("Tailscale refused the auth key")
	w.shows(`run "gangway login" again`)
	if strings.Contains(w.screen.raw(), tailscaleKey) {
		t.Fatal("the screen shows the auth key")
	}
	if len(control.hosts()) != before {
		t.Fatal("a host joined the Tailscale network")
	}
}

// localWorld gives both programs in the one-machine mode, with a stand-in
// Tailscale network in reach that neither should use. Nothing is started.
func localWorld(t *testing.T) (*world, *tailnet) {
	t.Helper()
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, "", "")
	w.ctlArgs, w.cliArgs = nil, nil
	return w, control
}

func TestHOST08_1_TheOneMachineModeNeedsNoTailscale(t *testing.T) {
	w, control := localWorld(t)
	w.startController()
	t.Cleanup(w.stopController)
	w.startCli(nil)
	got := w.run("echo local")
	got.ran(t, 0)
	if got.Output != "local\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
	w.nothingAsked()
	if strings.Contains(w.screen.text(), "Joining Tailscale") {
		t.Fatal("the screen says that the cli-tool joined Tailscale")
	}
	if n := control.calls.Load(); n != 0 || len(control.AllNodes()) != 0 {
		t.Fatalf("Tailscale got %d calls and has %d devices, want none", n, len(control.AllNodes()))
	}
	// The cli-tool reached the controller on localhost: it holds a
	// connection to the address the controller listens on.
	if !strings.Contains(w.screen.text(), "Connecting to "+w.address) {
		t.Fatal("the screen does not name the localhost address")
	}
	if !strings.Contains(w.screen.text(), "\n  network   connected, on this machine only\n") {
		t.Fatal("the screen does not say that the session is on this machine only")
	}
}

func TestHOST08_2_TheOneMachineModeIsForLocalhostOnly(t *testing.T) {
	beside(t)
	// A server that takes every call to this machine counts the calls.
	open, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	var calls atomic.Int64
	go func() {
		for {
			c, err := open.Accept()
			if err != nil {
				return
			}
			calls.Add(1)
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(open.Addr().String())

	hosts := []string{"0.0.0.0", "gangway-controller", "localhost.example.com"}
	for _, other := range otherAddresses(t) {
		if ip := net.ParseIP(other); ip != nil { // leaves out the ones tied to a network card
			hosts = append(hosts, other)
		}
	}
	for _, host := range hosts {
		w := &world{t: t, home: t.TempDir(), ctl: new(*exec.Cmd), noPasteLine: true,
			cliArgs: []string{"--local", "http://" + net.JoinHostPort(host, port)}}
		w.startCli(nil)
		w.ended()
		if code := w.cli.ProcessState.ExitCode(); code == 0 {
			t.Fatalf("%s: the cli-tool ended with exit code 0", host)
		}
		w.shows("the one-machine mode is for localhost only")
	}
	time.Sleep(quiet)
	if n := calls.Load(); n != 0 {
		t.Fatalf("the cli-tool made %d connections", n)
	}
}

// The local controller holds the address a cli-tool would pick in the
// one-machine mode, so the test cannot run beside the others that use it.
func TestHOST08_3_WithNoOptionTheSessionIsOnTheTailscaleController(t *testing.T) {
	defaultController(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, tailscaleKey)
	w.startTailscaleController()
	w.startCli(nil)

	got := w.run("echo which")
	got.ran(t, 0)
	local := &world{t: t, address: "http://127.0.0.1:8642"}
	if s := local.status(w.key, "POST", "/v1/hello", ""); s != 401 {
		t.Fatalf("the local controller answered %d to the session key, want 401: it has the session", s)
	}
	if len(control.hosts()) != 1 {
		t.Fatalf("the Tailscale network has %d hosts, want 1", len(control.hosts()))
	}
}

func TestHOST09_1_EveryStartIsANewDevice(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, tailscaleKey)
	w.cliConfig = filepath.Join(w.cliHome, "config")
	w.startTailscaleController()

	known := map[tailcfg.NodeID]bool{}
	seen := func() *tailcfg.Node {
		t.Helper()
		w.startCli(nil)
		w.run("echo live").ran(t, 0)
		var fresh []*tailcfg.Node
		for _, host := range control.hosts() {
			if !known[host.ID] {
				known[host.ID] = true
				fresh = append(fresh, host)
			}
		}
		if len(fresh) != 1 {
			t.Fatalf("the start brought %d new devices, want 1", len(fresh))
		}
		w.endByCtrlC()
		return fresh[0]
	}
	first := seen()
	second := seen()
	if len(control.hosts()) != 2 {
		t.Fatalf("Tailscale saw %d host devices, want 2", len(control.hosts()))
	}
	if first.ID == second.ID || first.Key == second.Key || first.Machine == second.Machine {
		t.Fatalf("the two starts share an identity: device %d and %d, key %v and %v, machine %v and %v",
			first.ID, second.ID, first.Key, second.Key, first.Machine, second.Machine)
	}
}

// Each program has a home folder of its own here, and the cli-tool's
// places for settings, state, cache, data and short-lived files are all
// inside its own. The test wrote the settings file itself. The cli-tool
// leaves one empty folder behind, which the library makes: "tailscale" in
// the user's data folder. The test lets it pass because it is empty. The
// log of the session is meant to stay, and is searched like every other
// file.
func TestHOST09_2_NothingOfTheDeviceStaysOnDisk(t *testing.T) {
	w, control := newTailscaleWorld(t)
	w.run("echo live > "+w.file("out")).ran(t, 0)
	host := control.hosts()[0]
	w.endByCtrlC()

	config := filepath.Dir(filepath.Dir(func() string {
		found, _ := filepath.Glob(filepath.Join(w.cliHome, "config*", "gangway", "tailscale-auth-key"))
		if len(found) != 1 {
			t.Fatalf("found %d auth key files, want 1", len(found))
		}
		return found[0]
	}()))
	allowed := map[string]bool{
		filepath.Join(config, "gangway", "tailscale-auth-key"): true,
		filepath.Join(config, "gangway", "settings"):           true,
	}
	marks := []string{"tskey", "tailscale", "nodekey:", "privkey:", "mkey:", host.Key.String(), host.Machine.String(), deviceNameOf(host), "stand-in.ts.net"}
	err := filepath.Walk(w.cliHome, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		log, _ := filepath.Match(filepath.Join(w.cliHome, "*", "gangway-*", "session.txt"), path)
		if !allowed[path] && !log {
			t.Errorf("the cli-tool left the file %s", path)
		}
		if path == filepath.Join(config, "gangway", "tailscale-auth-key") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, mark := range marks {
			if strings.Contains(strings.ToLower(string(b)), strings.ToLower(mark)) {
				t.Errorf("%s holds %q", path, mark)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
