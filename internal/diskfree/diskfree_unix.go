//go:build !windows

package diskfree

import "syscall"

// Free báo dung lượng trống (byte) của phân vùng chứa path.
func Free(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
