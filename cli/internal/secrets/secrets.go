// Package secrets keeps the Tailscale auth key on the host, in a file that
// only its owner can read. The design is in docs/design.md. The work is done by
// the code both programs share.
package secrets

import "github.com/kdop/gangway/shared/authkey"

// program names the folder of the file and the login to run.
const program = "gangway"

// File tells where the auth key file is.
func File() (string, error) { return authkey.File(program) }

// Save stores the key in the file at path, readable by its owner only.
func Save(path, key string) error { return authkey.Save(path, key) }

// Load reads the key from the file at path.
func Load(path string) (string, error) { return authkey.Load(path, program) }

// Remove takes the key file away. A file that is not there is no error.
func Remove(path string) error { return authkey.Remove(path) }

// Protected tells whether path leads to the auth key file, also through a
// link or through "..".
func Protected(file, path string) bool { return authkey.Protected(file, path) }
