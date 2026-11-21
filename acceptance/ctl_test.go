package acceptance

import (
	"bytes"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// otherAddress gives an address of this machine that another machine
// could reach, or skips the test when there is none.
func otherAddress(t *testing.T) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil && !ip.IP.IsLoopback() {
			return ip.IP.String()
		}
	}
	skip(t, "this machine has no address but localhost")
	return ""
}

// defaultController starts the controller with no setting. It skips the
// test when another program already holds the starting address.
func defaultController(t *testing.T) {
	ctl := exec.Command(controllerBin)
	var printed bytes.Buffer
	ctl.Stderr = &printed
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { ctl.Wait(); close(done) }()
	t.Cleanup(func() { ctl.Process.Kill(); <-done })
	for end := time.Now().Add(soon); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		select {
		case <-done:
			if strings.Contains(printed.String(), "address already in use") {
				skip(t, "another program holds 127.0.0.1:8642")
			}
			t.Fatalf("the controller did not start: %s", printed.String())
		default:
		}
		if c, err := net.Dial("tcp", "127.0.0.1:8642"); err == nil {
			c.Close()
			select {
			case <-done:
				continue
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}
	t.Fatal("the controller does not listen")
}

func TestCTL01_1_WithNoSettingTheControllerAnswersOnLocalhost(t *testing.T) {
	defaultController(t)
	resp, err := http.Post("http://127.0.0.1:8642/v1/hello", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("want the API to answer 401 to a call with no key, got %d", resp.StatusCode)
	}
}

// A call from another machine comes in at an address other than localhost.
// The test calls that address from this machine.
func TestCTL01_2_WithNoSettingTheControllerAnswersNowhereElse(t *testing.T) {
	other := otherAddress(t)
	defaultController(t)
	c, err := net.DialTimeout("tcp", net.JoinHostPort(other, "8642"), 2*time.Second)
	if err == nil {
		c.Close()
		t.Fatalf("the controller answers on %s", other)
	}
}

func TestCTL02_1_APublicAddressIsRefusedWithNoLogin(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:0", otherAddress(t) + ":0", "[::]:0"} {
		ctl := exec.Command(controllerBin, "-listen", listen)
		var printed bytes.Buffer
		ctl.Stderr = &printed
		done := make(chan error, 1)
		if err := ctl.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { done <- ctl.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("%s: the controller ended with no error", listen)
			}
		case <-time.After(3 * time.Second):
			ctl.Process.Kill()
			t.Fatalf("%s: the controller started", listen)
		}
		if strings.Contains(printed.String(), "listening on") || !strings.Contains(printed.String(), "login") {
			t.Fatalf("%s: want the reason and no start, got %q", listen, printed.String())
		}
	}
}

func TestCTL11_1_ARequestTheAPICannotReadIsTurnedAway(t *testing.T) {
	w := newWorld(t)
	for name, body := range map[string]string{
		"not JSON":       `touch ` + w.file("ran"),
		"no command":     `{"kind":"command"}`,
		"an empty one":   `{"kind":"command","command":""}`,
		"a kind unknown": `{"kind":"dance","command":"touch ` + w.file("ran") + `"}`,
		"no kind":        `{"command":"touch ` + w.file("ran") + `"}`,
	} {
		if s := w.status(w.key, "POST", "/v1/requests", body); s != 400 {
			t.Fatalf("%s: want 400, got %d", name, s)
		}
	}
	w.promptWaits(0)
	w.notRun("ran")
	w.live()
}

func TestCTL11_2_FieldsTheAPIDoesNotHaveChangeNothing(t *testing.T) {
	w := newWorld(t)
	body := `{"kind":"command","command":"touch ` + w.file("ran") + `","approved":true,"answer":"y","state":"approved"}`
	done := make(chan answer, 1)
	go func() {
		got, _ := w.call(background, "POST", "/v1/requests", body)
		done <- got
	}()
	w.prompt(1)
	w.shows("AI asks to run:\n  > touch " + w.file("ran") + "\n" + question)
	w.notRun("ran")
	select {
	case got := <-done:
		t.Fatalf("the AI got an answer with no key press: %d %s", got.Status, got.Body)
	default:
	}
	w.press("n")
	(<-done).refused(t, "user")
}
