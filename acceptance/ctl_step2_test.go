package acceptance

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kdop/gangway/shared/messages"
	"tailscale.com/tailcfg"
)

// pasteLineAddress gives the address in the paste line.
func (w *world) pasteLineAddress() string {
	w.t.Helper()
	for _, l := range strings.Split(w.screen.text(), "\n") {
		if f := strings.Fields(l); len(f) == 3 && f[0] == "/gangway" {
			return f[1]
		}
	}
	w.t.Fatal("the screen shows no paste line")
	return ""
}

func TestKEY10_1_ThePasteLineHoldsThePublicAddress(t *testing.T) {
	beside(t)
	const public = "https://gangway.example.com"
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, tailscaleKey)
	w.ctlArgs = append(w.ctlArgs, "-public-address", public)
	w.startTailscaleController()
	w.startCli(nil)
	if got := w.pasteLineAddress(); got != public {
		t.Fatalf("the paste line holds %s, want %s", got, public)
	}
}

// The cli-tool reaches the controller by its Tailscale name here, so the
// address in the paste line can only come from the controller.
func TestKEY10_2_ThePasteLineHoldsTheAddressTheAPIListensOn(t *testing.T) {
	w, _ := newTailscaleWorld(t)
	if got := w.pasteLineAddress(); got != "http://"+w.listen {
		t.Fatalf("the paste line holds %s, want http://%s", got, w.listen)
	}
}

func TestKEY10_3_AnAddressThatIsNotAPlainWebAddressIsNotShown(t *testing.T) {
	beside(t)
	for _, c := range []struct {
		name, address string
		bad           []string // what must never reach the terminal
	}{
		{"a cursor code", "http://127.0.0.1:1/\x1b[2Jwiped", []string{"\x1b[2J", "wiped"}},
		{"a space", "http://bad.example .com", []string{"bad.example"}},
		{"no http", "ftp://bad.example.com", []string{"ftp://", "bad.example"}},
		{"no http, with a script", "javascript:alert(1)", []string{"javascript", "alert"}},
		{"empty", "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := standInGiving(t, func(string) string { return c.address }, true)
			s.ended()
			if code := s.cli.ProcessState.ExitCode(); code == 0 {
				t.Error("the cli-tool ended with exit code 0")
			}
			text := s.screen.text()
			if !strings.Contains(text, "the controller gave an API address that cannot be used") {
				t.Errorf("the cli-tool does not say that it cannot use the address:\n%s", text)
			}
			if strings.Contains(text, "/gangway ") || strings.Contains(text, "Paste this line") {
				t.Errorf("a paste line is shown:\n%s", text)
			}
			for _, bad := range c.bad {
				if strings.Contains(s.screen.raw(), bad) {
					t.Errorf("%q reached the terminal", bad)
				}
			}
		})
	}
}

// hostDoorAnswer tries the hosts' door as a cli-tool would, with the
// client given, and tells what came back: the status of the answer, or 0
// when there was no answer at all.
func hostDoorAnswer(client *http.Client, address string) (status int, taken bool) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	conn, resp, err := websocket.Dial(ctx, address+messages.HostPath, &websocket.DialOptions{HTTPClient: client})
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil {
		return status, false
	}
	defer conn.CloseNow()
	limits := messages.Limits{CommandSeconds: 60, AnswerSeconds: 60, MaxWaiting: 20}
	first, _ := json.Marshal(messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash("gw_a-key-of-the-test"), Host: "fake", Limits: &limits})
	if conn.Write(ctx, websocket.MessageText, first) != nil {
		return status, false
	}
	var answer messages.Message
	_, b, err := conn.Read(ctx)
	return status, err == nil && json.Unmarshal(b, &answer) == nil && answer.Type == messages.TypeSessionOK
}

// noAnswerAt fails the test if anything takes a TCP connection there.
func noAnswerAt(t *testing.T, host string, port int) {
	t.Helper()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err == nil {
		c.Close()
		t.Errorf("something answers at %s, port %d", host, port)
	}
}

// The test has no second machine: "the machine's other addresses" are
// tried from this machine itself.
func TestCTL10_1_AHostIsRefusedOnEveryRoadButTailscale(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, tailscaleKey)
	w.startTailscaleController()
	// A host that comes over Tailscale is taken: the door does work.
	w.startCli(nil)
	w.run("echo over tailscale").ran(t, 0)

	// Over localhost, at the only port the controller holds there.
	if status, taken := hostDoorAnswer(nil, "ws://"+w.listen); taken || status != http.StatusNotFound {
		t.Errorf("a host at the API's address on localhost got the status %d, taken %v: want 404 and not taken", status, taken)
	}
	local := &world{t: t, home: t.TempDir(), listen: w.listen, address: w.address, ctl: w.ctl,
		cliArgs: []string{"--local", w.address}, noPasteLine: true}
	local.startCli(nil)
	local.ended()
	if code := local.cli.ProcessState.ExitCode(); code == 0 {
		t.Error("a cli-tool in the one-machine mode ended with exit code 0")
	}
	local.shows("cannot reach the controller")
	if strings.Contains(local.screen.text(), "/gangway ") {
		t.Error("a cli-tool in the one-machine mode got a session")
	}

	// The hosts' door is on no address of this machine.
	_, apiPort, _ := net.SplitHostPort(w.listen)
	port, _ := strconv.Atoi(apiPort)
	noAnswerAt(t, "127.0.0.1", messages.HostDoorPort)
	noAnswerAt(t, "::1", messages.HostDoorPort)
	for _, other := range otherAddresses(t) {
		noAnswerAt(t, other, messages.HostDoorPort)
		noAnswerAt(t, other, port)
	}
	if n := len(control.hosts()); n != 1 {
		t.Errorf("the Tailscale network has %d hosts, want 1", n)
	}
}

func TestCTL10_2_TheAPIDoesNotAnswerOnTheHostsDoor(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, "")
	w.startTailscaleController()
	client := control.device(t).HTTPClient()
	door := "http://" + net.JoinHostPort(messages.DefaultControllerName, strconv.Itoa(messages.HostDoorPort))

	for _, call := range []struct{ method, path string }{
		{"POST", "/v1/hello"},
		{"POST", "/v1/requests"},
		{"GET", "/v1/requests/r_1"},
		{"GET", "/"},
	} {
		ctx, stop := context.WithTimeout(context.Background(), joinWait)
		req, _ := http.NewRequestWithContext(ctx, call.method, door+call.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer gw_a-key-of-the-test")
		resp, err := client.Do(req)
		stop()
		if err != nil {
			t.Fatalf("%s %s over Tailscale: %v", call.method, call.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s on the hosts' door gave %d, want 404", call.method, call.path, resp.StatusCode)
		}
	}
	// The same paths do answer at the API's own address.
	if s := w.status("gw_a-key-of-the-test", "POST", "/v1/hello", ""); s != http.StatusUnauthorized {
		t.Errorf("the API gave %d at its own address, want 401", s)
	}
	// And the door does take a host that comes over Tailscale.
	if status, taken := hostDoorAnswer(client, door); !taken {
		t.Errorf("the hosts' door did not take a host over Tailscale: status %d", status)
	}
}

// The test has no second machine: "the machine's other addresses" are
// tried from this machine itself. A real cli-tool refuses such an address
// by itself, so the test knocks with a plain connection.
func TestCTL10_3_InTheOneMachineModeAHostComesOverLocalhostOnly(t *testing.T) {
	w := newWorld(t)
	w.run("echo taken").ran(t, 0)

	otherAddress(t)
	_, apiPort, _ := net.SplitHostPort(w.listen)
	port, _ := strconv.Atoi(apiPort)
	for _, other := range otherAddresses(t) {
		noAnswerAt(t, other, port)
	}
}

func TestCTL10_4_TheControllerSaysThatTheOneMachineModeIsOn(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	// The auth key is there: only the mode keeps the controller off
	// Tailscale.
	w := tailscaleParts(t, control, tailscaleKey, "")
	w.ctlArgs = []string{"-local", "-listen", w.listen}
	w.startController()
	t.Cleanup(w.stopController)

	w.until("the controller says that the mode is on", func() bool {
		said := w.ctlOut.text()
		return strings.Contains(said, "the one-machine mode is on") &&
			strings.Contains(said, "any program on this machine can start a session")
	})
	w.stays("no call reached Tailscale", func() bool { return control.calls.Load() == 0 })
	if _, err := os.Stat(w.ctlIdentityFolder()); err == nil {
		t.Fatal("the controller made a Tailscale identity")
	}
}

// ctlLoginWorld gives a controller where "gangway-controller login" has
// been done, as the user would do it. The controller is not started.
func ctlLoginWorld(t *testing.T) (*world, *tailnet) {
	t.Helper()
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, "", tailscaleKey)
	w.ctlLogin(tailscaleKey).loggedIn(tailscaleKey, ctlReady)
	return w, control
}

// The test does not look for a Tailscale installed on the machine. It
// gives the controller no way to use one: the network it joins is the
// stand-in, which only the program itself can reach.
func TestCTL12_1_AfterItsLoginTheControllerIsOnTailscale(t *testing.T) {
	w, control := ctlLoginWorld(t)
	w.startTailscaleController()
	if control.controller() == nil {
		t.Fatal("the Tailscale network has no device with the controller's name")
	}
	w.startCli(nil)
	got := w.run("echo reached")
	got.ran(t, 0)
	if got.Output != "reached\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
}

// The controller is started with no keyboard at all, so a start that
// asked for something would not get on.
func TestCTL12_2_ARestartAsksNothingAndIsTheSameDevice(t *testing.T) {
	w, control := ctlLoginWorld(t)
	device := func() *tailcfg.Node {
		t.Helper()
		w.startTailscaleController()
		if said := w.ctlOut.text(); strings.Contains(said, "Paste") {
			t.Fatalf("the controller asked for something: %s", said)
		}
		var all []*tailcfg.Node
		for _, node := range control.AllNodes() {
			if strings.HasPrefix(deviceNameOf(node), messages.DefaultControllerName) {
				all = append(all, node)
			}
		}
		if len(all) != 1 {
			t.Fatalf("the Tailscale network has %d devices of the controller, want 1", len(all))
		}
		return all[0]
	}
	first := device()
	w.stopController()
	second := device()
	if first.ID != second.ID || first.Key != second.Key || first.Machine != second.Machine {
		t.Fatalf("the restart made a new device: %d and %d, key %v and %v, machine %v and %v",
			first.ID, second.ID, first.Key, second.Key, first.Machine, second.Machine)
	}
	// A host reaches the controller after the restart.
	w.startCli(nil)
	w.run("echo again").ran(t, 0)
}

// login and the start run with a mask that leaves new files open to all:
// see onTerminal.
func TestCTL12_3_OnlyTheOwnerCanReadTheKeyAndTheIdentity(t *testing.T) {
	w, _ := ctlLoginWorld(t)
	check := func() {
		t.Helper()
		ownerOnly(t, w.ctlKeyFile(), 0o600)
		ownerOnly(t, w.ctlIdentityFolder(), 0o700)
		left, err := os.ReadDir(w.ctlIdentityFolder())
		if err != nil || len(left) == 0 {
			t.Fatalf("the identity folder holds nothing: %v", err)
		}
	}
	check()

	// The same after a start, which writes to the folder too.
	ctl := exec.Command("sh", "-c", `umask 0; exec "$0" "$@"`, controllerBin, "-listen", w.listen)
	ctl.Dir = w.home
	ctl.Env = append(append(os.Environ(), "HOME="+w.home), w.env...)
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ctl.Process.Kill(); ctl.Wait() })
	eventually(t, joinWait, "the controller listens", func() bool {
		c, err := net.Dial("tcp", w.listen)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
	check()
}

func TestCTL12_4_WithNoLoginTheControllerDoesNotStart(t *testing.T) {
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, "", "")
	w.launchController()
	done := make(chan error, 1)
	go func() { done <- (*w.ctl).Wait() }()
	select {
	case <-done:
	case <-time.After(soon):
		(*w.ctl).Process.Kill()
		t.Fatalf("the controller did not end. It said: %s", w.ctlOut.text())
	}
	if code := (*w.ctl).ProcessState.ExitCode(); code == 0 {
		t.Error("the controller ended with exit code 0")
	}
	said := w.ctlOut.text()
	if !strings.Contains(said, `run "gangway-controller login" first`) {
		t.Errorf("the controller does not say to run login: %s", said)
	}
	if strings.Contains(said, "listens") || strings.Contains(said, "one-machine mode") {
		t.Errorf("the controller started all the same: %s", said)
	}
	if c, err := net.DialTimeout("tcp", w.listen, time.Second); err == nil {
		c.Close()
		t.Error("the API answers")
	}
	if n := control.calls.Load(); n != 0 {
		t.Errorf("Tailscale got %d calls, want none", n)
	}
	if found, _ := filepath.Glob(filepath.Join(w.home, "ctl-*", "*", "*")); len(found) != 0 {
		t.Errorf("the controller wrote %v", found)
	}
}

// The test's own device calls the host over the Tailscale network, first
// while the session is live, to show that the call does get through.
func TestSES08_1_WhenTheSessionEndsTheHostLeavesTailscale(t *testing.T) {
	w, control := newTailscaleWorld(t)
	w.run("echo live").ran(t, 0)
	local, err := control.device(t).LocalClient()
	if err != nil {
		t.Fatal(err)
	}
	host := control.hosts()[0].Addresses[0].Addr()
	answers := func() bool {
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		got, err := local.Ping(ctx, host, tailcfg.PingDisco)
		return err == nil && got.Err == ""
	}
	eventually(t, joinWait, "the host answers on the Tailscale network while the session is live", answers)
	pid := w.cli.Process.Pid
	if _, udp := portsOf(t, pid); len(udp) == 0 {
		t.Fatal("the test is of no use: the cli-tool holds no port of the tunnel while the session is live")
	}

	w.endByCtrlC()
	// The cli-tool has ended, so every connection it held is closed.
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "fd")); err == nil {
		if tcp, udp := portsOf(t, pid); len(tcp)+len(udp) != 0 {
			t.Fatalf("the cli-tool still holds the ports %v %v", tcp, udp)
		}
	}
	for i := 0; i < 2; i++ {
		if answers() {
			t.Fatal("the host still answers on the Tailscale network")
		}
	}
}
