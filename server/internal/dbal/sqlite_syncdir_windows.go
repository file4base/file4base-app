package dbal

// syncDir does nothing on Windows: a directory cannot be opened for the
// write access FlushFileBuffers needs, and NTFS journals its names itself.
func syncDir(string) error { return nil }
