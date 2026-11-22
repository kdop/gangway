// Package secrets keeps the Tailscale auth key on the host, in a file that
// only its owner can read. The design is in design.md.
package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// keyStart starts every Tailscale auth key.
const keyStart = "tskey-auth-"

// File tells where the auth key file is.
func File() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gangway", "tailscale-auth-key"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gangway", "tailscale-auth-key"), nil
}

// check tells why the text cannot be an auth key. The error never holds
// the text.
func check(key string) error {
	if !strings.HasPrefix(key, keyStart) || len(key) == len(keyStart) {
		return errors.New("this is not a Tailscale auth key: one starts with " + keyStart)
	}
	for _, r := range key {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return errors.New("this is not a Tailscale auth key: it holds a space or a character that cannot be seen")
		}
	}
	return nil
}

// Save stores the key in the file at path, readable by its owner only.
func Save(path, key string) error {
	if err := check(key); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// The new file has its rights from the first moment, and takes the
	// place of the old one in one step.
	next := path + ".new"
	os.Remove(next)
	file, err := os.OpenFile(next, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(key + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(next, path)
	}
	if err != nil {
		os.Remove(next)
	}
	return err
}

// Load reads the key from the file at path.
func Load(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("this host has no Tailscale auth key yet: run \"gangway login\" first")
	}
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s can be read by others: run \"chmod 600 %s\", or run \"gangway login\" again", path, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(b))
	if check(key) != nil {
		return "", fmt.Errorf("%s does not hold a Tailscale auth key: run \"gangway login\" again", path)
	}
	return key, nil
}

// Remove takes the key file away. A file that is not there is no error.
func Remove(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Protected tells whether path leads to the auth key file, also through a
// link or through "..".
func Protected(file, path string) bool {
	if a, errA := os.Stat(file); errA == nil {
		if b, errB := os.Stat(path); errB == nil {
			return os.SameFile(a, b)
		}
	}
	return plain(file) == plain(path)
}

// plain gives the path with its links and its ".." worked out, as far as
// the path exists.
func plain(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(dir, filepath.Base(abs))
	}
	return abs
}
