package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Admin holds the bounds of the admin file. No layer below can widen
// them.
type Admin struct {
	SessionMax time.Duration // SESSION_MAX. Zero: no maximum
	File       string        // the file that was read, for the screen
}

// LoadAdmin reads the admin file. No folder or no file gives no bounds. A
// folder or a file that cannot be trusted or read gives an error: the
// cli-tool must then not start, because a bound may be in it.
func LoadAdmin() (Admin, error) {
	folder, owner := adminPlace()
	return loadAdmin(folder, owner)
}

// loadAdmin reads the file "settings" in folder. Both must belong to
// owner, and nobody else may be able to write them.
func loadAdmin(folder string, owner uint32) (Admin, error) {
	path := filepath.Join(folder, "settings")
	a := Admin{File: path}

	info, err := os.Lstat(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, fmt.Errorf("cannot look at %s: %w", folder, err)
	}
	if !info.IsDir() {
		return a, fmt.Errorf("%s must be a folder", folder)
	}
	if err := trusted(folder, info, owner); err != nil {
		return a, err
	}

	// Opened for reading only. The file is checked through the open file,
	// so it cannot be swapped between the check and the read.
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, fmt.Errorf("cannot read %s: %w", path, err)
	}
	defer file.Close()
	if info, err = file.Stat(); err != nil {
		return a, fmt.Errorf("cannot look at %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return a, fmt.Errorf("%s must be a plain file", path)
	}
	if err := trusted(path, info, owner); err != nil {
		return a, err
	}

	err = parse(file, path, func(name, value string) error {
		if name != "SESSION_MAX" {
			return fmt.Errorf("%q is not a setting of this file", name)
		}
		return setTime(&a.SessionMax, name, value, false)
	})
	if err != nil {
		return Admin{File: path}, err
	}
	return a, nil
}

// trusted tells whether only the owner can change what info describes.
func trusted(path string, info fs.FileInfo, owner uint32) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot tell who owns %s", path)
	}
	if stat.Uid != owner {
		return fmt.Errorf("%s must be owned by %s", path, ownerName(owner))
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s must be writable by %s only", path, ownerName(owner))
	}
	return nil
}

func ownerName(owner uint32) string {
	if owner == 0 {
		return "root"
	}
	return fmt.Sprintf("the user %d", owner)
}

// Length gives the length a session may start with when asked is asked
// for, and whether the maximum cut it.
func (a Admin) Length(asked time.Duration) (length time.Duration, cut bool) {
	if a.SessionMax > 0 && asked > a.SessionMax {
		return a.SessionMax, true
	}
	return asked, false
}
