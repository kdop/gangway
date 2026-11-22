package acceptance

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// heldPort is one port a program holds open for others to call.
type heldPort struct {
	kind    string // "tcp", "tcp6", "udp" or "udp6"
	address string // as the system writes it
	port    int
}

// portsOf lists the TCP ports a program listens on and the UDP ports it
// holds. It reads them from the system's own tables.
func portsOf(t *testing.T, pid int) (tcp, udp []heldPort) {
	t.Helper()
	links, err := filepath.Glob(filepath.Join("/proc", strconv.Itoa(pid), "fd", "*"))
	if err != nil || len(links) == 0 {
		t.Fatalf("cannot list the open files of the program %d", pid)
	}
	sockets := map[string]bool{}
	for _, link := range links {
		if to, err := os.Readlink(link); err == nil && strings.HasPrefix(to, "socket:[") {
			sockets[strings.TrimSuffix(strings.TrimPrefix(to, "socket:["), "]")] = true
		}
	}
	for _, kind := range []string{"tcp", "tcp6", "udp", "udp6"} {
		b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "net", kind))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 10 || !sockets[f[9]] {
				continue
			}
			at := strings.LastIndex(f[1], ":")
			port, err := strconv.ParseInt(f[1][at+1:], 16, 32)
			if err != nil {
				t.Fatalf("cannot read the line %q", line)
			}
			held := heldPort{kind: kind, address: f[1], port: int(port)}
			switch {
			case strings.HasPrefix(kind, "udp"):
				udp = append(udp, held)
			case f[3] == "0A": // the state "listen"
				tcp = append(tcp, held)
			}
		}
	}
	return tcp, udp
}
