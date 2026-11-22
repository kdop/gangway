package messages

import (
	"strings"
	"testing"
)

func TestPlainWebAddress(t *testing.T) {
	for _, good := range []string{
		"http://127.0.0.1:8642", "https://gangway.example.com", "https://example.com/gangway/", "http://[::1]:8642", "http://localhost:1",
	} {
		if !PlainWebAddress(good) {
			t.Errorf("%q was refused", good)
		}
	}
	for _, bad := range []string{
		"", "http://", "https://", "example.com", "ftp://example.com", "http:///path", "http://:80", "HTTP://example.com",
		"http://exa mple.com", "http://example.com\n", "http://example.com\x1b[2J", "http://example.com/​", "http://example.com/‮",
		"http://exämple.com", " http://example.com", "http://example.com\t", "http://" + strings.Repeat("a", 2000),
	} {
		if PlainWebAddress(bad) {
			t.Errorf("%q was taken", bad)
		}
	}
}
