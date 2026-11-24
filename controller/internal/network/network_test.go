package network

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tailscale.com/ipn/store/mem"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"

	"github.com/kdop/gangway/shared/authkey"
	"github.com/kdop/gangway/shared/designcheck"
)

const goodKey = "tskey-auth-fake-good-key"

// standIn is a stand-in for Tailscale's servers, from the library's own
// test tools.
type standIn struct {
	*testcontrol.Server
	url string
}

func newStandIn(t *testing.T) *standIn {
	t.Helper()
	// On a test machine the library must not try to pick a network card.
	netns.SetEnabled(false)
	control := &testcontrol.Server{
		DERPMap:        integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true},
		MagicDNSDomain: "stand-in.ts.net",
		RequireAuthKey: goodKey,
		Logf:           logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	return &standIn{Server: control, url: control.HTTPTestServer.URL}
}

// config gives a controller's folders in a scratch place, with this key
// stored. An empty key stores none.
func (s *standIn) config(t *testing.T, key string) Config {
	t.Helper()
	dir := t.TempDir()
	c := Config{
		KeyFile:     filepath.Join(dir, "config", Program, "tailscale-auth-key"),
		StateDir:    filepath.Join(dir, "state", Program),
		Name:        "gangway-controller",
		JoinTimeout: 20 * time.Second,
		ControlURL:  s.url,
	}
	if key != "" {
		if err := authkey.Save(c.KeyFile, key); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func join(t *testing.T, c Config) *Network {
	t.Helper()
	n, err := Join(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	return n
}

// host is a second device on the stand-in network, as a cli-tool is.
func (s *standIn) host(t *testing.T) *tsnet.Server {
	t.Helper()
	store, _ := mem.New(logger.Discard, "")
	h := &tsnet.Server{
		Dir: t.TempDir(), Store: store, Hostname: "gangway-host", Ephemeral: true,
		AuthKey: goodKey, ControlURL: s.url, Logf: logger.Discard, UserLogf: logger.Discard,
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	if _, err := h.Up(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func ownerOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("%s has the rights %o, want none for the group and for others", path, info.Mode().Perm())
	}
}

// UNIT-CTLNETWORK-01. Requirement: CTL-12.
func TestUNITCTLNETWORK01_JoinTakesTheControllersName(t *testing.T) {
	s := newStandIn(t)
	n := join(t, s.config(t, goodKey))
	nodes := s.AllNodes()
	if len(nodes) != 1 || !strings.HasPrefix(nodes[0].Name, "gangway-controller.") {
		t.Fatalf("the stand-in has %d devices, the first named %q", len(nodes), nodes[0].Name)
	}
	// The stand-in does not keep whether a device is short-lived, so the
	// test checks what was asked of the library.
	if n.server.Ephemeral {
		t.Fatal("the controller asked for a short-lived device")
	}
}

// UNIT-CTLNETWORK-02. Requirement: CTL-10.
func TestUNITCTLNETWORK02_AHostReachesTheDoorOverTailscale(t *testing.T) {
	s := newStandIn(t)
	n := join(t, s.config(t, goodKey))
	door, err := n.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer door.Close()
	go func() {
		for {
			c, err := door.Accept()
			if err != nil {
				return
			}
			io.WriteString(c, "the door")
			c.Close()
		}
	}()
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	conn, err := s.host(t).Dial(ctx, "tcp", "gangway-controller:8643")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got, _ := io.ReadAll(conn); string(got) != "the door" {
		t.Fatalf("the host read %q through the door", got)
	}
}

// UNIT-CTLNETWORK-03. Requirement: CTL-10.
func TestUNITCTLNETWORK03_TheDoorIsOnNoOtherRoad(t *testing.T) {
	s := newStandIn(t)
	n := join(t, s.config(t, goodKey))
	door, err := n.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer door.Close()
	addresses := []string{"127.0.0.1", "::1"}
	found, _ := net.InterfaceAddrs()
	for _, a := range found {
		if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLinkLocalUnicast() {
			addresses = append(addresses, ip.IP.String())
		}
	}
	for _, address := range addresses {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, "8643"), time.Second)
		if err == nil {
			conn.Close()
			t.Fatalf("the door answers at %s, off the Tailscale network", address)
		}
	}
}

// UNIT-CTLNETWORK-04. Requirement: CTL-12.
func TestUNITCTLNETWORK04_NoKeySaysToRunLogin(t *testing.T) {
	s := newStandIn(t)
	c := s.config(t, "")
	n, err := Join(context.Background(), c)
	if err == nil || n != nil || !strings.Contains(err.Error(), "gangway-controller login") {
		t.Fatalf("got %v, want an error that says to run the controller's login", err)
	}
	if s.NumNodes() != 0 {
		t.Fatal("a device joined with no key")
	}
	if _, err := os.Stat(c.StateDir); err == nil {
		t.Fatal("the state folder was made with no key")
	}
}

// UNIT-CTLNETWORK-05. Requirement: CTL-12.
func TestUNITCTLNETWORK05_TheSameDeviceAfterARestart(t *testing.T) {
	s := newStandIn(t)
	c := s.config(t, goodKey)
	identity := func() string {
		n, err := Join(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		defer n.Close()
		status, err := n.server.Up(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return status.Self.PublicKey.String()
	}
	first, second := identity(), identity()
	if first != second {
		t.Fatal("the controller is another device after a restart")
	}
	if s.NumNodes() != 1 {
		t.Fatalf("the stand-in has %d devices, want 1", s.NumNodes())
	}
}

// UNIT-CTLNETWORK-06. Requirement: CTL-12.
func TestUNITCTLNETWORK06_TheKeyAndTheIdentityAreForTheOwnerOnly(t *testing.T) {
	s := newStandIn(t)
	c := s.config(t, goodKey)
	// A state folder that was left open is closed again.
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	join(t, c)
	ownerOnly(t, c.KeyFile)
	ownerOnly(t, c.StateDir)
	inside, _ := os.ReadDir(c.StateDir)
	if len(inside) == 0 {
		t.Fatal("the identity is not kept in the state folder")
	}
}

// UNIT-CTLNETWORK-07. Requirement: CTL-12.
func TestUNITCTLNETWORK07_ARefusedKey(t *testing.T) {
	s := newStandIn(t)
	n, err := Join(context.Background(), s.config(t, "tskey-auth-fake-bad-key"))
	if !errors.Is(err, ErrKeyRefused) || !strings.Contains(err.Error(), "Tailscale said: ") || n != nil {
		t.Fatalf("got %v, want the error about a refused key", err)
	}
	if strings.Contains(err.Error(), "zzzz") || !strings.Contains(err.Error(), "gangway-controller login") {
		t.Fatalf("the error %q must say to run login and must not hold the key", err)
	}
}

// UNIT-CTLNETWORK-08. Requirement: CTL-12.
func TestUNITCTLNETWORK08_NoAnswerEndsAfterTheTimeout(t *testing.T) {
	// A server that takes the call and never answers.
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	netns.SetEnabled(false)
	c := (&standIn{url: "http://" + silent.Addr().String()}).config(t, goodKey)
	c.JoinTimeout = time.Second
	start := time.Now()
	n, err := Join(context.Background(), c)
	if err == nil || n != nil || !strings.Contains(err.Error(), "1s") || strings.Contains(err.Error(), goodKey) {
		t.Fatalf("got %v, want an error that names the timeout and not the key", err)
	}
	if took := time.Since(start); took > 6*time.Second {
		t.Fatalf("Join came back after %s", took)
	}
}

// UNIT-CTLNETWORK-09. Requirement: CTL-12.
func TestUNITCTLNETWORK09_AStateFolderThatCannotBeMade(t *testing.T) {
	s := newStandIn(t)
	c := s.config(t, goodKey)
	// A file stands where the folder must go.
	if err := os.MkdirAll(filepath.Dir(c.StateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.StateDir, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := Join(context.Background(), c)
	if err == nil || n != nil || strings.Contains(err.Error(), goodKey) {
		t.Fatalf("got %v, want an error that does not hold the key", err)
	}
	if s.NumNodes() != 0 {
		t.Fatal("a device joined")
	}
}

func TestStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/somewhere")
	if dir, err := StateDir(); err != nil || dir != "/somewhere/gangway-controller" {
		t.Fatalf("got %q, %v", dir, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/someone")
	if dir, _ := StateDir(); dir != "/home/someone/.local/state/gangway-controller" {
		t.Fatalf("with no XDG_STATE_HOME the folder is %q", dir)
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-CTLNETWORK", "network_test.go")
}
