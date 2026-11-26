package dbal

import (
	"strings"
)

// mountType is the filesystem type of the mount a path is on, read from the
// text of /proc/self/mountinfo: the type of the longest mount point the path
// is under. It is "" when no mount holds the path.
func mountType(mountinfo, path string) string {
	best, fstype := -1, ""
	for _, line := range strings.Split(mountinfo, "\n") {
		// id parent major:minor root mountpoint options [optional...] - fstype source super
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		fields := strings.Fields(left)
		after := strings.Fields(right)
		if len(fields) < 5 || len(after) < 1 {
			continue
		}
		point := unescapeMountPath(fields[4])
		if !underMountPoint(path, point) || len(point) <= best {
			continue
		}
		best, fstype = len(point), after[0]
	}
	return fstype
}

func underMountPoint(path, point string) bool {
	if point == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == point || strings.HasPrefix(path, point+"/")
}

// unescapeMountPath reverses the octal escapes mountinfo writes for a space,
// a tab, a newline and a backslash in a path.
func unescapeMountPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return '0' <= c && c <= '7' }

// unsafeMountType reports whether a mount type is one SQLite databases are
// refused on: network filesystems, FUSE, and the host-file sharing of Docker
// Desktop and WSL.
func unsafeMountType(fstype string) bool {
	if strings.HasPrefix(fstype, "fuse") || strings.HasPrefix(fstype, "nfs") {
		return true
	}
	switch fstype {
	case "cifs", "smb3", "smbfs", "9p", "virtiofs", "fakeowner", "drvfs", "vboxsf", "prl_fs",
		"vmhgfs", "afs", "ceph", "glusterfs", "lustre", "gpfs", "beegfs", "osxfs", "grpcfuse":
		return true
	}
	return false
}
