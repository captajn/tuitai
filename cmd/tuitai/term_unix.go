//go:build linux || darwin

package main

// term_unix.go: raw mode cho terminal Unix, khôi phục cấu hình khi thoát.

import (
	"os"
	"syscall"
)

type termState struct {
	fd   uintptr
	orig syscall.Termios
}

// termSetup đặt terminal về raw mode (tương đương cfmakeraw).
func termSetup() (func(), error) {
	fd := os.Stdin.Fd()
	var t syscall.Termios
	if err := ioctlTermios(fd, &t, false); err != nil {
		return func() {}, err
	}
	st := &termState{fd: fd, orig: t}

	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0

	if err := ioctlTermios(fd, &t, true); err != nil {
		return func() {}, err
	}
	return st.restore, nil
}

func (s *termState) restore() {
	orig := s.orig
	_ = ioctlTermios(s.fd, &orig, true)
}

// termWidth trả về số cột của terminal.
func termWidth() int {
	if w := envInt("COLUMNS"); w > 0 {
		return w
	}
	if w := termIoctlWidth(os.Stdout.Fd()); w > 0 {
		return w
	}
	return 100
}
