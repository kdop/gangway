package network

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"

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

// road gives a Tailscale road to the stand-in, with its folder.
func (s *standIn) road(t *testing.T, key string) (*tailscale, string) {
	t.Helper()
	folder := t.TempDir()
	n := Tailscale(Config{
		AuthKey: key, Host: "Web_Server 01", ControllerName: "gangway-controller",
		JoinTimeout: 20 * time.Second, Folder: folder, ControlURL: s.url,
	}).(*tailscale)
	t.Cleanup(n.Leave)
	return n, folder
}

func joined(t *testing.T) (*standIn, *tailscale, string) {
	t.Helper()
	s := newStandIn(t)
	n, folder := s.road(t, goodKey)
	if err := n.Join(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, n, folder
}

func empty(t *testing.T, folder string) {
	t.Helper()
	left, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("the folder still holds %d things, the first is %s", len(left), left[0].Name())
	}
}

// open lists the TCP ports of this program that take a connection from
// outside: they listen, they are not on a localhost address, and they are
// not tied to the loopback device.
func open(t *testing.T) []string {
	t.Helper()
	var found []string
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range fds {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if kind, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE); err != nil || kind != syscall.SOCK_STREAM {
			continue
		}
		if accepts, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ACCEPTCONN); err != nil || accepts != 1 {
			continue
		}
		var ip net.IP
		var port int
		switch a, _ := syscall.Getsockname(fd); a := a.(type) {
		case *syscall.SockaddrInet4:
			ip, port = net.IP(a.Addr[:]), a.Port
		case *syscall.SockaddrInet6:
			ip, port = net.IP(a.Addr[:]), a.Port
		default:
			continue
		}
		device, _ := unix.GetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE)
		if ip.IsLoopback() || strings.TrimRight(device, "\x00") == "lo" {
			continue
		}
		found = append(found, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	}
	return found
}

// UNIT-NETWORK-01. Requirement: HOST-08.
func TestUNITNETWORK01_OneMachineStartsAtTheStartingAddress(t *testing.T) {
	n, err := Local("")
	if err != nil || n.Address() != "http://127.0.0.1:8642" {
		t.Fatalf("got %v, %v, want http://127.0.0.1:8642", n, err)
	}
}

// UNIT-NETWORK-02. Requirement: HOST-08.
func TestUNITNETWORK02_OneMachineTakesLocalhost(t *testing.T) {
	for _, address := range []string{"http://localhost:9000", "http://127.0.0.1:9000", "http://[::1]:9000"} {
		n, err := Local(address)
		if err != nil || n.Address() != address {
			t.Fatalf("%s: got %v, %v", address, n, err)
		}
	}
}

// UNIT-NETWORK-03. Requirement: HOST-08.
func TestUNITNETWORK03_OneMachineRefusesAnythingElse(t *testing.T) {
	for _, address := range []string{
		"http://192.168.1.5:9000", "http://example.com", "http://0.0.0.0:9000", "not an address",
		"http://localhost.example.com", "http://127.0.0.1.example.com:9000", "ftp://127.0.0.1", "http://",
	} {
		if n, err := Local(address); err == nil {
			t.Fatalf("%s was taken: %v", address, n)
		}
	}
	if _, err := Local("http://192.168.1.5:9000"); !strings.Contains(err.Error(), "localhost only") {
		t.Fatalf("the error %q does not say the mode is for localhost only", err)
	}
}

// UNIT-NETWORK-04. Requirement: HOST-08.
func TestUNITNETWORK04_OneMachineJoinsNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	n, err := Local("")
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Join(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n.Client() != http.DefaultClient {
		t.Fatal("the one-machine road does not use the plain network")
	}
	n.Leave()
}

// UNIT-NETWORK-05. Requirement: HOST-06.
func TestUNITNETWORK05_JoinMakesOneShortLivedDevice(t *testing.T) {
	s, n, _ := joined(t)
	nodes := s.AllNodes()
	if len(nodes) != 1 {
		t.Fatalf("the stand-in has %d devices, want 1", len(nodes))
	}
	name := strings.TrimSuffix(nodes[0].Name, ".")
	if !strings.HasPrefix(name, "gangway-web-server-01") {
		t.Fatalf("the device is named %q, want gangway- and the host's name", name)
	}
	// The stand-in does not keep whether a device is short-lived, so the
	// test checks what was asked of the library.
	if !n.server.Ephemeral {
		t.Fatal("the cli-tool did not ask for a short-lived device")
	}
}

// UNIT-NETWORK-06. Requirement: HOST-06.
func TestUNITNETWORK06_TheAddressIsTheControllersName(t *testing.T) {
	_, n, _ := joined(t)
	if got := n.Address(); got != "http://gangway-controller:8643" {
		t.Fatalf("the address is %q", got)
	}
	if n.Client() == http.DefaultClient {
		t.Fatal("the Tailscale road uses the plain network")
	}
}

// UNIT-NETWORK-07. Requirement: HOST-09.
func TestUNITNETWORK07_EveryJoinIsANewDevice(t *testing.T) {
	s := newStandIn(t)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		n, _ := s.road(t, goodKey)
		if err := n.Join(context.Background()); err != nil {
			t.Fatal(err)
		}
		status, err := n.server.Up(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		seen[status.Self.PublicKey.String()] = true
		n.Leave()
	}
	if len(seen) != 2 {
		t.Fatalf("two joins gave %d identities, want 2", len(seen))
	}
}

// UNIT-NETWORK-08. Requirement: HOST-09, SES-08.
func TestUNITNETWORK08_LeaveRemovesTheFolderAndTheDevice(t *testing.T) {
	_, n, folder := joined(t)
	client := n.Client()
	n.Leave()
	empty(t, folder)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://gangway-controller:8643/", nil)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("the device still reaches the network after it left")
	}
	n.Leave()
}

// UNIT-NETWORK-09. Requirement: HOST-06.
func TestUNITNETWORK09_ARefusedKey(t *testing.T) {
	s := newStandIn(t)
	bad := "tskey-auth-fake-bad-key"
	n, folder := s.road(t, bad)
	err := n.Join(context.Background())
	if !errors.Is(err, ErrKeyRefused) || !strings.Contains(err.Error(), "Tailscale said: ") {
		t.Fatalf("got %v, want the error about a refused key", err)
	}
	if !strings.Contains(err.Error(), "gangway login") || strings.Contains(err.Error(), "zzzz") {
		t.Fatalf("the error %q must say to run login and must not hold the key", err)
	}
	empty(t, folder)
}

// UNIT-NETWORK-10. Requirement: HOST-06.
func TestUNITNETWORK10_NoAnswerEndsAfterTheTimeout(t *testing.T) {
	// A server that takes the call and never answers.
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	netns.SetEnabled(false)
	folder := t.TempDir()
	n := Tailscale(Config{
		AuthKey: goodKey, Host: "h", ControllerName: "gangway-controller",
		JoinTimeout: time.Second, Folder: folder, ControlURL: "http://" + silent.Addr().String(),
	})
	start := time.Now()
	err = n.Join(context.Background())
	if err == nil || !strings.Contains(err.Error(), "1s") || strings.Contains(err.Error(), goodKey) {
		t.Fatalf("got %v, want an error that names the timeout and not the key", err)
	}
	if took := time.Since(start); took > 6*time.Second {
		t.Fatalf("Join came back after %s", took)
	}
	empty(t, folder)
}

// UNIT-NETWORK-11. Requirement: HOST-03.
func TestUNITNETWORK11_NoPortListens(t *testing.T) {
	s := newStandIn(t)
	n, _ := s.road(t, goodKey)
	if before := open(t); len(before) != 0 {
		t.Fatalf("the test itself holds open ports: %v", before)
	}
	// The check must see a port that is open.
	wide, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	if seen := open(t); len(seen) != 1 {
		t.Fatalf("the check is of no use: it sees %v for one open port", seen)
	}
	wide.Close()
	if err := n.Join(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The library holds one TCP port for calls from this host itself. It
	// must be tied to the loopback device.
	if after := open(t); len(after) != 0 {
		t.Fatalf("after the join the program takes connections from outside on %v", after)
	}
}

// UNIT-NETWORK-12. Requirement: SES-08.
func TestUNITNETWORK12_LeaveDoesNotHang(t *testing.T) {
	s, n, folder := joined(t)
	// The stand-in goes away, so nothing answers the goodbye.
	s.HTTPTestServer.CloseClientConnections()
	s.HTTPTestServer.Close()
	start := time.Now()
	n.Leave()
	if took := time.Since(start); took > leaveWait+2*time.Second {
		t.Fatalf("Leave took %s", took)
	}
	empty(t, folder)
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-NETWORK", "network_test.go")
}
