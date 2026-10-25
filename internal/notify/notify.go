// Package notify thông báo khi job xong: lệnh tuỳ chỉnh, hoặc thông báo hệ thống
// mặc định (Windows toast qua PowerShell, macOS osascript, Linux notify-send).
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Config điều khiển thông báo.
type Config struct {
	// Enabled tắt/bật toàn bộ.
	Enabled bool
	// Command là mẫu lệnh tuỳ chỉnh, dùng {title} và {msg}. Ví dụ:
	//   powershell -c "New-BurntToastNotification -Text '{title}','{msg}'"
	Command string
}

// Notify phát thông báo; lỗi được bỏ qua (không làm hỏng luồng tải).
func Notify(cfg Config, title, msg string) error {
	if !cfg.Enabled {
		return nil
	}
	title = sanitize(title)
	msg = sanitize(msg)
	if cfg.Command != "" {
		line := strings.NewReplacer("{title}", title, "{msg}", msg).Replace(cfg.Command)
		return runShell(line)
	}
	switch runtime.GOOS {
	case "windows":
		ps := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType=WindowsRuntime] | Out-Null;` +
			`$t=[Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02);` +
			`$x=$t.GetElementsByTagName('text');$x.Item(0).AppendChild($t.CreateTextNode('` + title + `'))|Out-Null;` +
			`$x.Item(1).AppendChild($t.CreateTextNode('` + msg + `'))|Out-Null;` +
			`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Tui Download').Show([Windows.UI.Notifications.ToastNotification]::new($t))`
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", ps).Run()
	case "darwin":
		return exec.Command("osascript", "-e",
			`display notification "`+msg+`" with title "`+title+`"`).Run()
	default:
		return exec.Command("notify-send", title, msg).Run()
	}
}

// runShell chạy một dòng lệnh qua shell của nền tảng.
func runShell(line string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", line).Run()
	}
	return exec.Command("/bin/sh", "-c", line).Run()
}

// sanitize loại bỏ ký tự phá vỡ lệnh shell.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\'', '"', '`', '\n', '\r', ';', '&', '|', '$':
			return ' '
		}
		return r
	}, s)
}
