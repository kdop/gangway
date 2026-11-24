//go:build gangwaytest

package settings

import "os"

// adminPlace of a build for the acceptance tests, which cannot write
// /etc/gangway and are not root: the folder comes from the surroundings,
// and the owner is the user who runs the test. A program built this way
// must never be put on a host.
func adminPlace() (folder string, owner uint32) {
	if folder = os.Getenv("GANGWAY_TEST_ADMIN_FOLDER"); folder != "" {
		return folder, uint32(os.Geteuid())
	}
	return "/etc/gangway", 0
}
