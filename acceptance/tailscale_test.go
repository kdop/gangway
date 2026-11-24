package acceptance

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

const tailscaleKey = "tskey-auth-fake-good-key"

// otherTailscaleKey is a key the stand-in network does not take.
const otherTailscaleKey = "tskey-auth-fake-other-key"

// joinWait is how long a test waits for a program to get onto the
// stand-in network.
const joinWait = 40 * time.Second

// tailnet is the stand-in for Tailscale's servers that the library ships.
// It counts the calls it gets.
type tailnet struct {
	*testcontrol.Server
	calls atomic.Int64
}

func tailscaleStandIn(t *testing.T) *tailnet {
	t.Helper()
	n := &tailnet{Server: &testcontrol.Server{
		DERPMap:        integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true},
		MagicDNSDomain: "stand-in.ts.net",
		RequireAuthKey: tailscaleKey,
		Logf:           logger.Discard,
	}}
	n.HTTPTestServer = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.calls.Add(1)
		n.Server.ServeHTTP(w, r)
	}))
	n.HTTPTestServer.Start()
	t.Cleanup(n.HTTPTestServer.Close)
	return n
}

// device gives the name of a device as it was asked for: the stand-in
// adds its own ending.
func deviceNameOf(node *tailcfg.Node) string { return strings.SplitN(node.Name, ".", 2)[0] }

// hosts gives the devices that are neither the controller nor the test's
// own device.
func (n *tailnet) hosts() []*tailcfg.Node {
	var hosts []*tailcfg.Node
	for _, node := range n.AllNodes() {
		if name := deviceNameOf(node); name != "gangway-controller" && !strings.HasPrefix(name, testDeviceName) {
			hosts = append(hosts, node)
		}
	}
	return hosts
}

// controller gives the controller's device, or nil.
func (n *tailnet) controller() *tailcfg.Node {
	for _, node := range n.AllNodes() {
		if deviceNameOf(node) == "gangway-controller" {
			return node
		}
	}
	return nil
}

const testDeviceName = "test-device"

var inProcessOnce sync.Once

// device puts the test itself on the stand-in network, as one more device.
func (n *tailnet) device(t *testing.T) *tsnet.Server {
	t.Helper()
	inProcessOnce.Do(func() {
		// On a test machine the library must not try to pick a network card.
		netns.SetEnabled(false)
		envknob.SetNoLogsNoSupport()
	})
	store, err := mem.New(logger.Discard, "")
	if err != nil {
		t.Fatal(err)
	}
	d := &tsnet.Server{
		Dir:        t.TempDir(),
		Store:      store,
		Hostname:   testDeviceName,
		Ephemeral:  true,
		AuthKey:    tailscaleKey,
		ControlURL: n.HTTPTestServer.URL,
		Logf:       logger.Discard,
		UserLogf:   logger.Discard,
	}
	t.Cleanup(func() { d.Close() })
	ctx, stop := context.WithTimeout(context.Background(), joinWait)
	defer stop()
	if _, err := d.Up(ctx); err != nil {
		t.Fatalf("the test's own device did not get onto the stand-in network: %v", err)
	}
	return d
}

// tailscaleParts gives a world on the stand-in network with nothing
// started yet. Each program has a home folder of its own. A key that is
// not empty is stored as that program's login would store it.
func tailscaleParts(t *testing.T, control *tailnet, ctlKey, cliKey string) *world {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()

	home := t.TempDir()
	host := filepath.Join(home, "host")
	if err := os.MkdirAll(filepath.Join(host, "short-lived"), 0o700); err != nil {
		t.Fatal(err)
	}
	w := &world{
		t: t, home: home, listen: listen, address: "http://" + listen, ctl: new(*exec.Cmd),
		ctlArgs: []string{"-listen", listen},
		cliArgs: []string{},
		cliKey:  cliKey,
		cliHome: host,
		env: []string{
			"TS_CONTROL_URL=" + control.HTTPTestServer.URL,
			"XDG_CONFIG_HOME=" + filepath.Join(home, "ctl-config"),
			"XDG_STATE_HOME=" + filepath.Join(home, "ctl-state"),
		},
		cliEnv: []string{
			"XDG_STATE_HOME=" + filepath.Join(host, "state"),
			"XDG_CACHE_HOME=" + filepath.Join(host, "cache"),
			"XDG_DATA_HOME=" + filepath.Join(host, "data"),
			"TMPDIR=" + filepath.Join(host, "short-lived"),
		},
	}
	if ctlKey != "" {
		storeKey(t, w.ctlKeyFile(), ctlKey)
	}
	return w
}

func (w *world) ctlKeyFile() string {
	return filepath.Join(w.home, "ctl-config", "gangway-controller", "tailscale-auth-key")
}

func (w *world) ctlIdentityFolder() string {
	return filepath.Join(w.home, "ctl-state", "gangway-controller")
}

// storeKey writes an auth key file as login does.
func storeKey(t *testing.T, file, key string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newTailscaleWorld starts both real programs on the stand-in Tailscale
// network. The controller is started with no option, and the cli-tool too.
func newTailscaleWorld(t *testing.T) (*world, *tailnet) {
	t.Helper()
	beside(t)
	control := tailscaleStandIn(t)
	w := tailscaleParts(t, control, tailscaleKey, tailscaleKey)
	w.startTailscaleController()
	w.startCli(nil)
	return w, control
}

// startTailscaleController starts the controller and waits until it has
// joined and listens.
func (w *world) startTailscaleController() {
	w.t.Helper()
	w.launchController()
	w.t.Cleanup(w.stopController)
	eventually(w.t, joinWait, "the controller listens", func() bool {
		c, err := net.Dial("tcp", w.listen)
		if err == nil {
			c.Close()
		}
		return err == nil
	})
}

// eventually is until with a wait of the caller's choice.
func eventually(t *testing.T, wait time.Duration, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("waited in vain: %s", what)
}

// program is a program that runs on a terminal of its own.
type program struct {
	t      *testing.T
	screen *screen
	keys   *os.File
	cmd    *exec.Cmd
	done   chan struct{}
}

// onTerminal starts a program on a terminal of its own. The program
// starts with a mask that leaves every new file open to all, so a file
// that must be closed to others is closed by the program itself.
func onTerminal(t *testing.T, dir string, env []string, bin string, args ...string) *program {
	t.Helper()
	master, slave := openTerminal(t)
	p := &program{t: t, screen: &screen{}, keys: master, done: make(chan struct{})}
	p.cmd = exec.Command("sh", append([]string{"-c", `umask 0; exec "$0" "$@"`, bin}, args...)...)
	p.cmd.Dir = dir
	p.cmd.Env = env
	p.cmd.Stdin, p.cmd.Stdout, p.cmd.Stderr = slave, slave, slave
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	slave.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			p.screen.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	go func() { p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		p.cmd.Process.Kill()
		<-p.done
		master.Close()
		if t.Failed() {
			t.Logf("the screen of %s %s:\n%s", filepath.Base(bin), strings.Join(args, " "), p.screen.text())
		}
	})
	return p
}

// shows waits until the screen holds the text.
func (p *program) shows(text string) {
	p.t.Helper()
	eventually(p.t, joinWait, "the screen shows "+text, func() bool { return strings.Contains(p.screen.text(), text) })
}

// exits waits for the program to end and gives its exit code.
func (p *program) exits() int {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(joinWait):
		p.t.Fatal("the program did not end")
	}
	return p.cmd.ProcessState.ExitCode()
}

// login runs a program's login on a terminal and pastes the key.
func login(t *testing.T, dir string, env []string, bin, key string) *program {
	t.Helper()
	p := onTerminal(t, dir, env, bin, "login")
	p.shows("Paste the Tailscale auth key")
	if _, err := p.keys.Write([]byte(key + "\r")); err != nil {
		t.Fatal(err)
	}
	return p
}

// loggedIn fails the test unless the login went well and never showed
// the key.
func (p *program) loggedIn(key, ready string) {
	p.t.Helper()
	if code := p.exits(); code != 0 {
		p.t.Fatalf("login ended with exit code %d", code)
	}
	if !strings.Contains(p.screen.text(), ready) {
		p.t.Fatalf("login did not say: %s", ready)
	}
	if strings.Contains(p.screen.raw(), key) || strings.Contains(p.screen.raw(), key[len(key)-8:]) {
		p.t.Fatal("login showed the auth key on the screen")
	}
}

// cliLogin does "gangway login" with the key, as the user would.
func (w *world) cliLogin(key string) *program {
	w.t.Helper()
	if w.cliConfig == "" {
		w.cliConfig = filepath.Join(w.cliHome, "config")
	}
	return login(w.t, w.cliHome, w.cliEnviron(w.cliConfig), cliBin, key)
}

func (w *world) cliKeyFile() string {
	return filepath.Join(w.cliConfig, "gangway", "tailscale-auth-key")
}

// ctlLogin does "gangway-controller login" with the key.
func (w *world) ctlLogin(key string) *program {
	w.t.Helper()
	return login(w.t, w.home, append(append(os.Environ(), "HOME="+w.home), w.env...), controllerBin, key)
}

const (
	cliReady = "This host is ready."
	ctlReady = "The controller is ready."
)

func TestHOST06_1_ASessionOverTailscale(t *testing.T) {
	w, control := newTailscaleWorld(t)
	got := w.run("echo over tailscale")
	got.ran(t, 0)
	if got.Output != "over tailscale\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
	if !strings.Contains(w.screen.text(), "Joining Tailscale ...") || !strings.Contains(w.screen.text(), "\n  network   connected over Tailscale\n") {
		t.Fatal("the screen does not say that the cli-tool joined Tailscale")
	}
	if control.controller() == nil || len(control.hosts()) != 1 || len(control.AllNodes()) != 2 {
		t.Fatalf("the stand-in network has %d devices, want the controller and one host", len(control.AllNodes()))
	}
}
