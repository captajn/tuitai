//go:build windows

package diskfree

// Free trả về số byte còn trống trên phân vùng chứa path (Windows).
import (
	"syscall"
	"unsafe"
)

// Free báo dung lượng trống (byte) của ổ chứa path.
func Free(path string) (uint64, error) {
	var (
		freeBytesAvailable     uint64
		totalNumberOfBytes     uint64
		totalNumberOfFreeBytes uint64
	)
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	r, _, e := proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if r == 0 {
		return 0, e
	}
	return freeBytesAvailable, nil
}
