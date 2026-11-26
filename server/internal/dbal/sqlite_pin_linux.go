//go:build linux

package dbal

import (
	"os"

	"golang.org/x/sys/unix"
)

// pinSQLiteFile opens the database file as a path only (O_PATH), to keep
// its inode from being reused while a pool is open on it. A descriptor
// opened for reading would not do: closing it would release the POSIX
// locks SQLite's own connections hold on the file in this process. Linux
// releases none when an O_PATH descriptor is closed.
func pinSQLiteFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
