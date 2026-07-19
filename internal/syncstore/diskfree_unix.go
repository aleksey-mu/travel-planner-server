//go:build unix

package syncstore

import "golang.org/x/sys/unix"

// FreeBytes returns available bytes on the filesystem containing path (Unix).
func FreeBytes(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	bsize := int64(st.Bsize)
	if st.Frsize > 0 {
		bsize = int64(st.Frsize)
	}
	return int64(st.Bavail) * bsize, nil
}
