//go:build !linux

package dbal

import "os"

// pinSQLiteFile pins nothing outside Linux: closing a descriptor of the
// file would release the POSIX locks SQLite's connections hold on it, and
// there is no O_PATH. A pool tells a replaced file by its inode alone.
func pinSQLiteFile(string) (*os.File, error) {
	return nil, os.ErrInvalid
}
