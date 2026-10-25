//go:build windows

// Package tray tạo icon khay hệ thống trên Windows bằng Win32 API thuần
// (Shell_NotifyIconW) — không cần CGO, không thư viện ngoài.
package tray

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// Callbacks là các hành động từ menu khay.
type Callbacks struct {
	Open      func()
	PauseAll  func()
	ResumeAll func()
	Quit      func()
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pLoadImageW          = user32.NewProc("LoadImageW")
	pUnregisterClassW    = user32.NewProc("UnregisterClassW")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmApp           = 0x8000
	wmTrayCallback  = wmApp + 1
	wmClose         = 0x0010
	wmDestroy       = 0x0002
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	niifInfo = 0x01

	mfString = 0x0000
	tpmRet   = 0x0100
	tpmRight = 0x0002

	imageIcon      = 1
	lrLoadFromFile = 0x0010
)

type point struct{ x, y int32 }

type msgT struct {
	hwnd     syscall.Handle
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type notifyIconDataW struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

// Tray là icon khay đang chạy.
type Tray struct {
	hwnd  syscall.Handle
	icon  syscall.Handle
	nid   notifyIconDataW
	cb    Callbacks
	class string
	ready chan error
}

var (
	active   *Tray
	exStyle  = uintptr(0)
	menuIDs  = struct{ open, pause, resume, quit uintptr }{1, 2, 3, 4}
	errNoWin = errors.New("tray: không tạo được cửa sổ ẩn")
)

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func copyUTF16(dst []uint16, s string) {
	src := syscall.StringToUTF16(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(dst)-1] = 0
	}
	copy(dst, src)
}

// Start tạo icon khay và chạy vòng lặp thông điệp ở goroutine riêng.
// iconPath trỏ tới file .ico; cb có thể nil.
func Start(iconPath, tip string, cb Callbacks) (*Tray, error) {
	t := &Tray{cb: cb, class: "TuiTaiTrayWnd", ready: make(chan error, 1)}
	go func() {
		runtime.LockOSThread()
		if err := t.run(iconPath, tip); err != nil {
			t.ready <- err
		}
	}()
	select {
	case err := <-t.ready:
		return nil, err
	default:
	}
	// chờ ngắn để biết cửa sổ đã tạo hay chưa
	if err, ok := <-t.ready; ok {
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *Tray) run(iconPath, tip string) error {
	hInst, _, _ := pGetModuleHandleW.Call(0)
	wc := wndClassExW{
		style:         0x0002, // CS_HREDRAW|CS_VREDRAW
		lpfnWndProc:   syscall.NewCallback(t.wndProc),
		hInstance:     syscall.Handle(hInst),
		lpszClassName: utf16Ptr(t.class),
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.ready <- err
		return err
	}
	hwnd, _, _ := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(wc.lpszClassName)),
		0, 0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		t.ready <- errNoWin
		return errNoWin
	}
	t.hwnd = syscall.Handle(hwnd)

	if iconPath != "" {
		if p, e := syscall.UTF16PtrFromString(iconPath); e == nil {
			h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrLoadFromFile)
			t.icon = syscall.Handle(h)
		}
	}

	nid := notifyIconDataW{
		hWnd:             t.hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayCallback,
		hIcon:            t.icon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.szTip[:], tip)
	t.nid = nid
	if r, _, e := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); r == 0 {
		t.ready <- e
		return e
	}
	active = t
	t.ready <- nil

	var m msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if m.message == wmClose {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	pDestroyWindow.Call(uintptr(t.hwnd))
	pUnregisterClassW.Call(uintptr(unsafe.Pointer(wc.lpszClassName)), hInst)
	return nil
}

func (t *Tray) wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayCallback:
		switch uint32(lParam) & 0xFFFF {
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		case wmLButtonDblClk:
			if t.cb.Open != nil {
				go t.cb.Open()
			}
		}
		return 0
	case wmClose, wmDestroy:
		pPostMessageW.Call(uintptr(hwnd), 0x0012 /*WM_QUIT*/, 0, 0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func (t *Tray) showMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	appendItem := func(id uintptr, text string) {
		pAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	appendItem(menuIDs.open, "Mở Tui Tải")
	appendItem(menuIDs.pause, "Tạm dừng tất cả")
	appendItem(menuIDs.resume, "Chạy tiếp tất cả")
	appendItem(menuIDs.quit, "Thoát")

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(uintptr(t.hwnd))
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRet|tpmRight, uintptr(pt.x), uintptr(pt.y), 0,
		uintptr(t.hwnd), 0)
	switch cmd {
	case menuIDs.open:
		if t.cb.Open != nil {
			go t.cb.Open()
		}
	case menuIDs.pause:
		if t.cb.PauseAll != nil {
			go t.cb.PauseAll()
		}
	case menuIDs.resume:
		if t.cb.ResumeAll != nil {
			go t.cb.ResumeAll()
		}
	case menuIDs.quit:
		if t.cb.Quit != nil {
			go t.cb.Quit()
		}
		t.Stop()
	}
}

// Notify hiện balloon/toast kèm icon app (kiểu IDM).
func (t *Tray) Notify(title, text string) {
	if t == nil {
		return
	}
	n := t.nid
	n.uFlags = nifInfo
	copyUTF16(n.szInfoTitle[:], title)
	copyUTF16(n.szInfo[:], text)
	n.dwInfoFlags = niifInfo
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

// Stop gỡ icon và kết thúc vòng lặp.
func (t *Tray) Stop() {
	if t == nil || t.hwnd == 0 {
		return
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	pPostMessageW.Call(uintptr(t.hwnd), wmClose, 0, 0)
}

// Supported báo nền tảng có tray không.
func Supported() bool { return true }
