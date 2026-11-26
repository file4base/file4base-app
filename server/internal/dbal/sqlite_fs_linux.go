//go:build linux

package dbal

import (
	"os"
	"path/filepath"
	"syscall"
)

// Filesystem magic numbers (statfs f_type) SQLite databases are refused on.
var unsafeSQLiteFilesystems = map[int64]string{
	0x6969:     "NFS",
	0x517B:     "SMB",
	0xFF534D42: "CIFS",
	0xFE534D42: "SMB2",
	0x01021997: "9p",
	0x65735546: "FUSE (virtiofs, gRPC FUSE, sshfs...)",
	0x5346414F: "AFS",
	0x00C36400: "CephFS",
}

// Filesystems named in diagnostics when they are safe.
var knownSQLiteFilesystems = map[int64]string{
	0xEF53:     "ext4",
	0x58465342: "xfs",
	0x9123683E: "btrfs",
	0x2FC12FC1: "zfs",
	0x01021994: "tmpfs",
	0x794C7630: "overlayfs",
	0xF2F52010: "f2fs",
}

// sqliteFilesystem reads the filesystem type twice: from statfs, and from
// the mount table, which names the filesystems that have no magic number of
// their own or share a local one (Docker Desktop's "fakeowner", WSL's
// "drvfs"). Either one finding an unsafe filesystem refuses the directory.
func sqliteFilesystem(dir string) (string, bool) {
	name := ""
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err == nil {
		magic := int64(st.Type) & 0xFFFFFFFF
		if n, ok := unsafeSQLiteFilesystems[magic]; ok {
			return n, true
		}
		name = knownSQLiteFilesystems[magic]
	}
	if mountinfo, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		abs, err := filepath.Abs(dir)
		if err == nil {
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				abs = resolved
			}
			if fstype := mountType(string(mountinfo), abs); fstype != "" {
				if unsafeMountType(fstype) {
					return fstype, true
				}
				if name == "" {
					name = fstype
				}
			}
		}
	}
	return name, false
}
