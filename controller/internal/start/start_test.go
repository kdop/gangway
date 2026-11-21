package start

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kdop/gangway/shared/designcheck"
	"github.com/kdop/gangway/shared/messages"
)

// UNIT-CTLSTART-01. Requirement: CTL-01.
func TestUNITCTLSTART01_TheAddressWithNoSetting(t *testing.T) {
	if err := CheckListen(messages.DefaultListen); err != nil {
		t.Fatalf("the default address is refused: %v", err)
	}
	hostName, _, err := net.SplitHostPort(messages.DefaultListen)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(hostName); ip == nil || !ip.IsLoopback() {
		t.Fatalf("the default address %q is not a loopback address", messages.DefaultListen)
	}
}

// UNIT-CTLSTART-02. Requirement: CTL-01.
func TestUNITCTLSTART02_ThisMachinePasses(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1", "127.9.9.9:1", "[::1]:1", "localhost:1", "127.0.0.1:0"} {
		if err := CheckListen(addr); err != nil {
			t.Errorf("%q is refused: %v", addr, err)
		}
	}
}

// UNIT-CTLSTART-03. Requirement: CTL-02.
func TestUNITCTLSTART03_EveryOtherAddressIsRefused(t *testing.T) {
	for _, addr := range []string{
		":8642", "0.0.0.0:8642", "[::]:8642", "192.168.1.5:8642", "10.0.0.1:8642",
		"100.64.0.1:8642", "8.8.8.8:80", "example.com:80", "localhost.example.com:80",
		"8642", "", "127.0.0.1", "[::ffff:8.8.8.8]:80",
	} {
		if err := CheckListen(addr); err == nil {
			t.Errorf("%q passes", addr)
		}
	}
}

// UNIT-CTLSTART-04. Requirement: CTL-02.
func TestUNITCTLSTART04_TheProgramDoesNotStartOnAnOpenAddress(t *testing.T) {
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run([]string{"-listen", "0.0.0.0:0"}, &out) }()
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("the exit code is 0")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the program started and did not end")
	}
	if !strings.Contains(out.String(), "no login is set up") {
		t.Fatalf("the reason is not printed: %q", out.String())
	}
	if strings.Contains(out.String(), "listening") {
		t.Fatalf("it listened: %q", out.String())
	}
}

// UNIT-CTLSTART-05. Requirement: CTL-01.
func TestUNITCTLSTART05_OneListenerTwoDoors(t *testing.T) {
	c := New()
	server := httptest.NewServer(c.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the API gave %d, want 401", resp.StatusCode)
	}

	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+messages.HostPath, nil)
	if err != nil {
		t.Fatalf("the door did not take the websocket: %v", err)
	}
	conn.CloseNow()

	// The API does not take a websocket, and the door is at one path only.
	for _, path := range []string{"/v1/host", "/", "/host/", "/host/v1/more", "/host/v2", "/v1/session"} {
		if conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, nil); err == nil {
			conn.CloseNow()
			t.Fatalf("a websocket was taken at %s", path)
		}
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-CTLSTART", "start_test.go")
}
