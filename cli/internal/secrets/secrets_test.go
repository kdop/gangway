package secrets

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdop/gangway/shared/designcheck"
)

// The work is done and tested in shared/authkey. This checks that the
// cli-tool asks for its own file and names its own login.
func TestTheCliToolsOwnFileAndLogin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere")
	file, err := File()
	if err != nil || file != "/somewhere/gangway/tailscale-auth-key" {
		t.Fatalf("the file is %q, %v", file, err)
	}
	path := filepath.Join(t.TempDir(), "gangway", "tailscale-auth-key")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), `"gangway login"`) {
		t.Fatalf("with no file the user is told %v", err)
	}
	key := "tskey-auth-kTESTONLY11CNTRL-abcdefghijklmnopqrstuvwxyz"
	if err := Save(path, key); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || got != key {
		t.Fatalf("Load gave %q, %v", got, err)
	}
	if !Protected(path, filepath.Join(filepath.Dir(path), "..", "gangway", "tailscale-auth-key")) {
		t.Fatal("a path to the file through .. is not protected")
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestDesignAndGoTestsMatch(t *testing.T) {
	designcheck.Match(t, "UNIT-SECRETS", "../../../shared/authkey/authkey_test.go")
}
