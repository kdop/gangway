package acceptance

import (
	"strings"
	"testing"
)

// The four calls that need the caller token. The id is made up: a call
// that is let in gets 404 for it, one that is not gets 401.
func (w *world) callsButHello() []struct{ method, path, body string } {
	return []struct{ method, path, body string }{
		{"GET", "/v1/session", ""},
		{"POST", "/v1/requests", `{"kind":"command","command":"touch ` + w.file("ran") + `"}`},
		{"POST", "/v1/requests/r_1/wait", ""},
		{"POST", "/v1/requests/r_1/stop", ""},
	}
}

// refusedAs fails the test unless every call with the session key and this
// caller token is turned away, a second hello too, and nothing reaches the
// host.
func (w *world) refusedAs(caller string) {
	w.t.Helper()
	before := w.prompts()
	noKey, err := w.callAs(background, "", "", "GET", "/v1/session", "")
	if err != nil {
		w.t.Fatal(err)
	}
	calls := append(w.callsButHello(), struct{ method, path, body string }{"POST", "/v1/hello", ""})
	for _, c := range calls {
		got, err := w.callAs(background, w.key, caller, c.method, c.path, c.body)
		if err != nil {
			w.t.Fatal(err)
		}
		if got.Status != 401 || got.Body != noKey.Body {
			w.t.Fatalf("%s %s gave %d %s, want the 401 of a call with no key", c.method, c.path, got.Status, got.Body)
		}
		if got.CallerToken != "" {
			w.t.Fatalf("%s %s handed out a caller token", c.method, c.path)
		}
	}
	w.promptWaits(before)
	w.notRun("ran")
}

func TestKEY05_1_TheFirstCallGetsACallerToken(t *testing.T) {
	w := newWorld(t)
	got := w.hello()
	if got.Status != 200 || !strings.HasPrefix(got.CallerToken, "gwc_") || len(got.CallerToken) != len("gwc_")+64 {
		t.Fatalf("the first hello gave %d %s, want 200 with a caller token", got.Status, strings.ReplaceAll(got.Body, got.CallerToken, "gwc_<token>"))
	}
	if got.CallerToken == w.key || strings.Contains(w.screen.text(), got.CallerToken) {
		t.Fatal("the caller token is the key, or it is on the host's screen")
	}
}

func TestKEY05_2_TheKeyAndTheTokenAreAccepted(t *testing.T) {
	w := newWorld(t)
	token := w.hello().CallerToken
	got, err := w.callAs(background, w.key, token, "GET", "/v1/session", "")
	if err != nil || got.Status != 200 {
		t.Fatalf("session with the key and the token gave %d %s %v, want 200", got.Status, got.Body, err)
	}
	ran := w.run("echo with the token")
	ran.ran(t, 0)
	if ran.Output != "with the token\n" {
		t.Fatalf("want the output, got %s", ran.Body)
	}
}

func TestKEY05_3_TheKeyWithNoTokenIsRefused(t *testing.T) {
	w := newWorld(t)
	w.hello()
	w.refusedAs("")
	// The first caller still owns the session.
	w.run("echo still mine").ran(t, 0)
}

func TestKEY05_4_TheKeyWithAWrongTokenIsRefused(t *testing.T) {
	w := newWorld(t)
	token := w.hello().CallerToken
	other := newWorld(t)
	for _, wrong := range []string{
		"gwc_" + strings.Repeat("0", 64),
		token[:len(token)-1],
		token + "0",
		strings.TrimPrefix(token, "gwc_"),
		strings.ToUpper(token),
		other.hello().CallerToken,
		w.key,
	} {
		w.refusedAs(wrong)
	}
	w.run("echo still mine").ran(t, 0)
}
