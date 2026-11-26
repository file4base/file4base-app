//go:build linux

package dbal

import "syscall"

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

func sqliteFilesystem(dir string) (string, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return "", false
	}
	magic := int64(st.Type) & 0xFFFFFFFF
	if name, ok := unsafeSQLiteFilesystems[magic]; ok {
		return name, true
	}
	if name, ok := knownSQLiteFilesystems[magic]; ok {
		return name, false
	}
	return "", false
}
