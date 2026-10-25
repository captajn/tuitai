//go:build linux

package main

// term_linux.go: ioctl termios/winsize của Linux.

import (
	"syscall"
	"unsafe"
)

type winsize struct{ row, col, xpixel, ypixel uint16 }

func ioctlTermios(fd uintptr, t *syscall.Termios, set bool) error {
	req := uintptr(syscall.TCGETS)
	if set {
		req = syscall.TCSETS
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func termIoctlWidth(fd uintptr) int {
	var ws winsize
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)), 0, 0, 0)
	if errno != 0 {
		return 0
	}
	return int(ws.col)
}
