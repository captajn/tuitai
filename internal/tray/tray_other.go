//go:build !windows

// Package tray: trên macOS/Linux chưa có icon khay thuần Go (cần CGO/DBus),
// nên app chạy nền không tray. Các hàm dưới đây giữ API thống nhất.
package tray

import "errors"

// Callbacks là các hành động từ menu khay.
type Callbacks struct {
	Open      func()
	PauseAll  func()
	ResumeAll func()
	Quit      func()
}

// ErrUnsupported báo nền tảng không hỗ trợ tray trong bản build này.
var ErrUnsupported = errors.New("tray: nền tảng này chưa hỗ trợ icon khay (dùng macOS/Linux: app chạy nền, mở UI bằng trình duyệt)")

// Tray là stub.
type Tray struct{}

// Start luôn trả ErrUnsupported.
func Start(iconPath, tip string, cb Callbacks) (*Tray, error) { return nil, ErrUnsupported }

// Notify không làm gì.
func (t *Tray) Notify(title, text string) {}

// Stop không làm gì.
func (t *Tray) Stop() {}

// Supported báo false.
func Supported() bool { return false }
