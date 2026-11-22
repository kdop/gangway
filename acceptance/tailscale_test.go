package acceptance

import (
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

const tailscaleKey = "tskey-auth-kGOODKEY11CNTRL-abcdefghijklmnop"

// newTailscaleWorld starts both real programs on a stand-in Tailscale
// network: the stand-in for Tailscale's servers that the library ships.
// The controller is started with no option, and the cli-tool too.
func newTailscaleWorld(t *testing.T) (*world, *testcontrol.Server) {
	t.Helper()
	control := &testcontrol.Server{
		DERPMap:        integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true},
		MagicDNSDomain: "stand-in.ts.net",
		RequireAuthKey: tailscaleKey,
		Logf:           logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	ln.Close()

	home := t.TempDir()
	config, state := filepath.Join(home, "ctl-config"), filepath.Join(home, "ctl-state")
	keyFile := filepath.Join(config, "gangway-controller", "tailscale-auth-key")
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte(tailscaleKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &world{
		t: t, home: home, listen: listen, address: "http://" + listen, ctl: new(*exec.Cmd),
		ctlArgs: []string{"-listen", listen},
		cliArgs: []string{},
		cliKey:  tailscaleKey,
		env: []string{
			"TS_CONTROL_URL=" + control.HTTPTestServer.URL,
			"XDG_CONFIG_HOME=" + config,
			"XDG_STATE_HOME=" + state,
		},
	}
	w.startController()
	t.Cleanup(w.stopController)
	w.startCli(nil)
	return w, control
}

func TestHOST06_1_ASessionOverTailscale(t *testing.T) {
	w, control := newTailscaleWorld(t)
	got := w.run("echo over tailscale")
	got.ran(t, 0)
	if got.Output != "over tailscale\n" {
		t.Fatalf("want the output, got %s", got.Body)
	}
	if !strings.Contains(w.screen.text(), "Joining Tailscale ...") {
		t.Fatal("the screen does not say that the cli-tool joined Tailscale")
	}
	names := map[string]bool{}
	for _, node := range control.AllNodes() {
		names[strings.SplitN(node.Name, ".", 2)[0]] = true
	}
	if len(names) != 2 || !names["gangway-controller"] {
		t.Fatalf("the stand-in network has the devices %v, want the controller and one host", names)
	}
}
