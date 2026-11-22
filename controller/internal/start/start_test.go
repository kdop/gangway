package start

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// syncBuffer is a buffer that the program writes and the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// started runs the program with these options and gives what it printed
// once it listens. It gives the address it listens on.
func started(t *testing.T, args ...string) (printed *syncBuffer, address string) {
	t.Helper()
	printed = &syncBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- run(ctx, args, printed) }()
	t.Cleanup(func() { stop(); <-done })
	for i := 0; i < 500; i++ {
		for _, line := range strings.Split(printed.String(), "\n") {
			if a, ok := strings.CutPrefix(line, "gangway-controller: listening on "); ok && strings.Contains(printed.String(), "one-machine") {
				return printed, a
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the program did not start: %q", printed.String())
	return nil, ""
}

// apiAddress starts a session as a host would and gives the API address
// that session_ok holds.
func apiAddress(t *testing.T, address string) string {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	conn, _, err := websocket.Dial(ctx, "ws://"+address+messages.HostPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	limits := messages.Limits{CommandSeconds: 60, AnswerSeconds: 60, MaxWaiting: 20}
	first, _ := json.Marshal(messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash("gw_" + strings.Repeat("a", 64)), Host: "h", Limits: &limits})
	if err := conn.Write(ctx, websocket.MessageText, first); err != nil {
		t.Fatal(err)
	}
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var answer messages.Message
	if json.Unmarshal(b, &answer) != nil || answer.Type != messages.TypeSessionOK {
		t.Fatalf("got %s, want session_ok", b)
	}
	return answer.APIAddress
}

// UNIT-CTLSTART-06. Requirement: CTL-10.
func TestUNITCTLSTART06_TheOneMachineModeSaysItIsOn(t *testing.T) {
	printed, _ := started(t, "-local", "-listen", "127.0.0.1:0")
	if !strings.Contains(printed.String(), "the one-machine mode is on: any program on this machine can start a session") {
		t.Fatalf("the start text is %q", printed.String())
	}
}

// UNIT-CTLSTART-07. Requirement: KEY-10.
func TestUNITCTLSTART07_TheAPIAddressGivenToAHost(t *testing.T) {
	_, address := started(t, "-local", "-listen", "127.0.0.1:0")
	if got := apiAddress(t, address); got != "http://"+address {
		t.Fatalf("with no setting the host got %q, want http://%s", got, address)
	}
	_, address = started(t, "-local", "-listen", "127.0.0.1:0", "-public-address", "https://gangway.example.com")
	if got := apiAddress(t, address); got != "https://gangway.example.com" {
		t.Fatalf("the host got %q, want the address that was set", got)
	}
}

// UNIT-CTLSTART-08. Requirement: KEY-10.
func TestUNITCTLSTART08_ABadPublicAddressStopsTheStart(t *testing.T) {
	for _, bad := range []string{"gangway.example.com", "https://exa mple.com", "ftp://example.com", "https://example.com/\x1b[2J", "http://"} {
		var out bytes.Buffer
		if code := Run([]string{"-local", "-listen", "127.0.0.1:0", "-public-address", bad}, &out); code == 0 {
			t.Fatalf("%q: the exit code is 0", bad)
		}
		if !strings.Contains(out.String(), "-public-address") || strings.Contains(out.String(), "listening") {
			t.Fatalf("%q: want the setting named and no start, got %q", bad, out.String())
		}
	}
}
