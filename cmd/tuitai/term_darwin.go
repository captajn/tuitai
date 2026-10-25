//go:build darwin

package main

// term_darwin.go: ioctl termios/winsize của macOS.

import (
	"syscall"
	"unsafe"
)

// Hằng số ioctl BSD (không có sẵn trong syscall trên darwin).
const (
	tioCGetA   = 0x40487413 // TIOCGETA
	tioCSetA   = 0x80487414 // TIOCSETA
	tioCGwinsz = 0x40087468 // TIOCGWINSZ
)

type winsize struct{ row, col, xpixel, ypixel uint16 }

func ioctlTermios(fd uintptr, t *syscall.Termios, set bool) error {
	req := uintptr(tioCGetA)
	if set {
		req = tioCSetA
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func termIoctlWidth(fd uintptr) int {
	var ws winsize
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, tioCGwinsz, uintptr(unsafe.Pointer(&ws)), 0, 0, 0)
	if errno != 0 {
		return 0
	}
	return int(ws.col)
}
