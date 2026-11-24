//go:build !gangwaytest

package settings

// adminPlace gives the folder of the admin file and the user who must own
// it. Both are fixed in the program: no setting, option or variable of
// the surroundings moves them, because a command approved by mistake
// could set any of those.
func adminPlace() (folder string, owner uint32) {
	return "/etc/gangway", 0
}
