package start

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"

	"github.com/kdop/gangway/controller/internal/network"
	"github.com/kdop/gangway/shared/authkey"
	"github.com/kdop/gangway/shared/messages"
)

const goodKey = "tskey-auth-kGOODKEY11CNTRL-abcdefghijklmnop"

// standIn starts a stand-in for Tailscale's servers and points the
// program at it, with scratch folders for its key and its identity.
func standIn(t *testing.T) (control *testcontrol.Server, keyFile string) {
	t.Helper()
	// On a test machine the library must not try to pick a network card.
	netns.SetEnabled(false)
	control = &testcontrol.Server{
		DERPMap:        integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1"),
		DNSConfig:      &tailcfg.DNSConfig{Proxied: true},
		MagicDNSDomain: "stand-in.ts.net",
		RequireAuthKey: goodKey,
		Logf:           logger.Discard,
	}
	control.HTTPTestServer = httptest.NewUnstartedServer(control)
	control.HTTPTestServer.Start()
	t.Cleanup(control.HTTPTestServer.Close)
	home := t.TempDir()
	t.Setenv("TS_CONTROL_URL", control.HTTPTestServer.URL)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	keyFile, err := authkey.File(network.Program)
	if err != nil {
		t.Fatal(err)
	}
	return control, keyFile
}

// UNIT-CTLSTART-09. Requirement: CTL-10.
func TestUNITCTLSTART09_TwoDoorsThatDoNotMix(t *testing.T) {
	c := New()
	api := httptest.NewServer(c.APIHandler())
	defer api.Close()
	hosts := httptest.NewServer(c.HostHandler())
	defer hosts.Close()

	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(api.URL, "http")+messages.HostPath, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the hosts' path on the API's handler gave %v, %v, want not found", resp, err)
	}
	got, err := http.Get(hosts.URL + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	if got.StatusCode != http.StatusNotFound {
		t.Fatalf("an API path on the hosts' handler gave %d, want 404", got.StatusCode)
	}
	// Each still does its own job.
	if conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hosts.URL, "http")+messages.HostPath, nil); err != nil {
		t.Fatalf("the hosts' handler does not take a host: %v", err)
	} else {
		conn.CloseNow()
	}
	if got, err := http.Get(api.URL + "/v1/session"); err != nil || got.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the API's handler does not answer the API: %v, %v", got, err)
	}
}

// UNIT-CTLSTART-10. Requirement: CTL-12.
func TestUNITCTLSTART10_NoKeyNoStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := free.Addr().String()
	free.Close()

	var out bytes.Buffer
	if code := Run([]string{"-listen", listen}, &out); code == 0 {
		t.Fatal("the exit code is 0")
	}
	if !strings.Contains(out.String(), "gangway-controller login") || strings.Contains(out.String(), "listens on") {
		t.Fatalf("want a text that says to run login, and no start, got %q", out.String())
	}
	if conn, err := net.DialTimeout("tcp", listen, time.Second); err == nil {
		conn.Close()
		t.Fatal("the API listens")
	}
}

// sessionStart opens the hosts' path with dial and sends session_start.
// It gives the answer.
func sessionStart(t *testing.T, url string, client *http.Client) (messages.Message, *http.Response, error) {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	conn, resp, err := websocket.Dial(ctx, url+messages.HostPath, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return messages.Message{}, resp, err
	}
	defer conn.CloseNow()
	limits := messages.Limits{CommandSeconds: 60, AnswerSeconds: 60, MaxWaiting: 20}
	first, _ := json.Marshal(messages.Message{Type: messages.TypeSessionStart, KeyHash: messages.KeyHash("gw_" + strings.Repeat("b", 64)), Host: "h", Limits: &limits, LengthSeconds: 1800})
	if err := conn.Write(ctx, websocket.MessageText, first); err != nil {
		t.Fatal(err)
	}
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var answer messages.Message
	json.Unmarshal(b, &answer)
	return answer, resp, nil
}

// UNIT-CTLSTART-11. Requirement: CTL-10, KEY-10.
func TestUNITCTLSTART11_AHostComesInOverTailscaleOnly(t *testing.T) {
	control, keyFile := standIn(t)
	if err := authkey.Save(keyFile, goodKey); err != nil {
		t.Fatal(err)
	}
	printed := &syncBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-listen", "127.0.0.1:0"}, printed) }()
	t.Cleanup(func() { stop(); <-done })
	var api string
	for i := 0; i < 2000 && api == ""; i++ {
		for _, line := range strings.Split(printed.String(), "\n") {
			if a, ok := strings.CutPrefix(line, "gangway-controller: the API listens on "); ok && strings.Contains(printed.String(), "on Tailscale") {
				api = a
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if api == "" {
		t.Fatalf("the program did not start: %q", printed.String())
	}
	if strings.Contains(printed.String(), "one-machine") {
		t.Fatalf("the start text speaks of the one-machine mode: %q", printed.String())
	}

	store, _ := mem.New(logger.Discard, "")
	host := &tsnet.Server{
		Dir: t.TempDir(), Store: store, Hostname: "gangway-host", Ephemeral: true,
		AuthKey: goodKey, ControlURL: control.HTTPTestServer.URL, Logf: logger.Discard, UserLogf: logger.Discard,
	}
	up, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := host.Up(up); err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	answer, _, err := sessionStart(t, "http://gangway-controller:8643", host.HTTPClient())
	if err != nil || answer.Type != messages.TypeSessionOK || answer.APIAddress != "http://"+api {
		t.Fatalf("over Tailscale the host got %+v, %v, want session_ok with the API address http://%s", answer, err, api)
	}
	_, resp, err := sessionStart(t, "http://"+api, http.DefaultClient)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("on the API's listener the host got %v, %v, want not found", resp, err)
	}
}

// UNIT-CTLSTART-12. Requirement: CTL-12.
func TestUNITCTLSTART12_LoginStoresAndTriesTheKey(t *testing.T) {
	_, keyFile := standIn(t)
	config, err := tailscaleConfig("gangway-controller")
	if err != nil {
		t.Fatal(err)
	}
	oldKey := "tskey-auth-kOLDKEY11CNTRL-aaaaaaaaaaaaaaaa"
	if err := authkey.Save(keyFile, oldKey); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		key, err := authkey.Load(keyFile, network.Program)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}

	var screen bytes.Buffer
	bad := "tskey-auth-kBADKEY11CNTRL-zzzzzzzzzzzz"
	err = Login(context.Background(), config, bad, &screen)
	if err == nil || !strings.Contains(err.Error(), "Tailscale refused the key") || stored() != oldKey {
		t.Fatalf("a refused key gave %v, and the stored key changed: %v", err, stored() != oldKey)
	}
	if err := Login(context.Background(), config, goodKey, &screen); err != nil || stored() != goodKey {
		t.Fatalf("a good key gave %v, stored: %v", err, stored() == goodKey)
	}
	if !strings.Contains(screen.String(), "The controller is ready") {
		t.Fatalf("the screen does not say the controller is ready: %q", screen.String())
	}
	if strings.Contains(screen.String()+err2text(err), "zzzz") || strings.Contains(screen.String(), "abcdefghijklmnop") {
		t.Fatal("a key is on the screen")
	}
}

func err2text(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// UNIT-CTLSTART-13. Requirement: CTL-12.
func TestUNITCTLSTART13_ABusyAPIAddressStopsTheStart(t *testing.T) {
	control, keyFile := standIn(t)
	if err := authkey.Save(keyFile, goodKey); err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	var out bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run([]string{"-listen", busy.Addr().String()}, &out) }()
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("the exit code is 0")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the program did not end")
	}
	if !strings.Contains(out.String(), "address already in use") || strings.Contains(out.String(), "listens on") {
		t.Fatalf("want the reason and no start, got %q", out.String())
	}
	// The controller had joined before it found the address busy. It must
	// have left again: its door no longer answers.
	store, _ := mem.New(logger.Discard, "")
	host := &tsnet.Server{
		Dir: t.TempDir(), Store: store, Hostname: "gangway-host", Ephemeral: true,
		AuthKey: goodKey, ControlURL: control.HTTPTestServer.URL, Logf: logger.Discard, UserLogf: logger.Discard,
	}
	up, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := host.Up(up); err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	dial, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if conn, err := host.Dial(dial, "tcp", "gangway-controller:8643"); err == nil {
		conn.Close()
		t.Fatal("the hosts' door still answers after the start failed")
	}
}

// UNIT-CTLSTART-14. Requirement: CTL-12.
func TestUNITCTLSTART14_WordsThatMakeNoSense(t *testing.T) {
	for _, args := range [][]string{{"login", "now"}, {"start"}, {"-local", "extra"}, {"-no-such-option"}} {
		var out bytes.Buffer
		if code := Run(args, &out); code != 2 {
			t.Fatalf("%v: the exit code is %d, want 2", args, code)
		}
		if !strings.Contains(out.String(), "gangway-controller login") || strings.Contains(out.String(), "gangway-controller: the API listens on") {
			t.Fatalf("%v: want the text on how to start, got %q", args, out.String())
		}
	}
}
