package dbal

import "testing"

// Docker Desktop's file sharing and WSL's Windows drives have no magic
// number statfs would refuse: the mount table names them.
func TestSQLite_MountTypeOfAPath(t *testing.T) {
	mountinfo := `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
30 22 0:26 / /data rw,relatime shared:2 - ext4 /dev/sdb1 rw
31 30 0:27 / /data/shared rw,relatime - fakeowner shared rw
32 22 0:28 / /mnt/c rw,relatime - 9p drvfs rw
33 22 0:29 / /var/lib/with\040space rw - fuse.sshfs host: rw
34 22 0:30 / /datax rw - nfs4 srv:/x rw
`
	cases := map[string]string{
		"/":                         "ext4",
		"/home/user":                "ext4",
		"/data/solutions":           "ext4",
		"/data/shared":              "fakeowner",
		"/data/shared/solutions":    "fakeowner",
		"/data/sharedx":             "ext4",
		"/mnt/c/Users/x":            "9p",
		"/var/lib/with space/files": "fuse.sshfs",
		"/datax/y":                  "nfs4",
	}
	for path, want := range cases {
		if got := mountType(mountinfo, path); got != want {
			t.Errorf("mountType(%q) = %q, want %q", path, got, want)
		}
	}
	for fstype, unsafe := range map[string]bool{
		"ext4": false, "xfs": false, "btrfs": false, "zfs": false, "overlay": false, "tmpfs": false,
		"fakeowner": true, "virtiofs": true, "fuse.sshfs": true, "fuse": true, "9p": true,
		"nfs4": true, "cifs": true, "smb3": true, "drvfs": true, "grpcfuse": true,
	} {
		if got := unsafeMountType(fstype); got != unsafe {
			t.Errorf("unsafeMountType(%q) = %v, want %v", fstype, got, unsafe)
		}
	}
}
