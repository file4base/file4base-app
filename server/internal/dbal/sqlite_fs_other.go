//go:build !linux

package dbal

// sqliteFilesystem is unknown outside Linux, where the desktop build keeps
// its databases in the user's local application data.
func sqliteFilesystem(string) (string, bool) { return "", false }
