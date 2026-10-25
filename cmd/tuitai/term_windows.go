//go:build windows

package main

// term_windows.go: bật VT processing + tắt line input/echo trên console Windows.

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode   = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode   = kernel32.NewProc("SetConsoleMode")
	procGetScreenBufInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const (
	enableProcessedOutput = 0x0001
	enableVTProcOutput    = 0x0004
	enableWrapAtEOL       = 0x0002
	enableLineInput       = 0x0002
	enableEchoInput       = 0x0004
)

type consoleCoord struct{ x, y int16 }

type consoleRect struct{ left, top, right, bottom int16 }

type consoleScreenBufferInfo struct {
	size    consoleCoord
	cursor  consoleCoord
	attrs   uint16
	window  consoleRect
	maxSize consoleCoord
}

type termState struct {
	inHandle  uintptr
	outHandle uintptr
	inOrig    uint32
	outOrig   uint32
	ok        bool
}

func consoleMode(proc *syscall.LazyProc, handle uintptr) (uint32, bool) {
	var mode uint32
	r, _, _ := proc.Call(handle, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return 0, false
	}
	return mode, true
}

// termSetup bật VT, tắt line input + echo để đọc từng phím.
func termSetup() (func(), error) {
	st := &termState{}
	st.inHandle = os.Stdin.Fd()
	st.outHandle = os.Stdout.Fd()

	if mode, okSP := consoleMode(procGetConsoleMode, st.outHandle); okSP {
		st.outOrig = mode
		st.ok = true
		newMode := mode | enableProcessedOutput | enableVTProcOutput | enableWrapAtEOL
		procSetConsoleMode.Call(st.outHandle, uintptr(newMode))
	}
	if mode, okIN := consoleMode(procGetConsoleMode, st.inHandle); okIN {
		st.inOrig = mode
		procSetConsoleMode.Call(st.inHandle, uintptr(mode&^uint32(enableLineInput|enableEchoInput)))
	}
	return st.restore, nil
}

func (s *termState) restore() {
	if !s.ok {
		return
	}
	if s.outOrig != 0 {
		procSetConsoleMode.Call(s.outHandle, uintptr(s.outOrig))
	}
	if s.inOrig != 0 {
		procSetConsoleMode.Call(s.inHandle, uintptr(s.inOrig))
	}
}

// termWidth trả về số cột hiện tại của cửa sổ console.
func termWidth() int {
	if w := envInt("COLUMNS"); w > 0 {
		return w
	}
	if w := screenBufferWidth(os.Stdout.Fd()); w > 0 {
		return w
	}
	return 100
}

func screenBufferWidth(handle uintptr) int {
	var info consoleScreenBufferInfo
	r, _, _ := procGetScreenBufInfo.Call(handle, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.window.right) - int(info.window.left) + 1
}
