//go:build !windows

package dbal

import "os"

// syncDir makes the names a directory holds durable: a file created, linked
// or removed in it is there, or gone, after a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
