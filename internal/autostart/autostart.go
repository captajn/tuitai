// Package autostart bật/tắt chạy cùng hệ điều hành.
//
// Windows: HKCU\Software\Microsoft\Windows\CurrentVersion\Run
// macOS:   ~/Library/LaunchAgents/com.captajn.tuitai.plist
// Linux:   ~/.config/autostart/tuitai.desktop
package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const appID = "com.captajn.tuitai"
const winValueName = "TuiTai"

// Command trả về lệnh sẽ chạy khi khởi động (đường dẫn exe hiện tại).
func Command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// Windows cần dấu ngoặc kép bọc đường dẫn, KHÔNG escape dấu \.
	return `"` + exe + `" gui --no-open --tray`, nil
}

// Enable bật chạy cùng hệ điều hành.
func Enable() error {
	switch runtime.GOOS {
	case "windows":
		cmd, err := Command()
		if err != nil {
			return err
		}
		out, err := exec.Command("reg", "add",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", winValueName, "/t", "REG_SZ", "/d", cmd, "/f").CombinedOutput()
		if err != nil {
			return fmt.Errorf("autostart: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	case "darwin":
		p, err := plistPath()
		if err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>gui</string><string>--no-open</string><string>--tray</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
</dict></plist>
`, appID, exe)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
		_ = exec.Command("launchctl", "unload", p).Run()
		return exec.Command("launchctl", "load", p).Run()
	default: // linux, *bsd
		p, err := desktopPath()
		if err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		body := "[Desktop Entry]\nType=Application\nName=Tui Tải\nComment=Trình quản lý tải xuống\n" +
			"Exec=" + exe + " gui --no-open --tray\nX-GNOME-Autostart-enabled=true\nTerminal=false\n"
		return os.WriteFile(p, []byte(body), 0o644)
	}
}

// Disable tắt chạy cùng hệ điều hành.
func Disable() error {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("reg", "delete",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", winValueName, "/f").CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if strings.Contains(strings.ToLower(msg), "unable to find") ||
				strings.Contains(strings.ToLower(msg), "không tìm thấy") {
				return nil // vốn chưa bật
			}
			return fmt.Errorf("autostart: %v: %s", err, msg)
		}
		return nil
	case "darwin":
		p, err := plistPath()
		if err != nil {
			return err
		}
		_ = exec.Command("launchctl", "unload", p).Run()
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	default:
		p, err := desktopPath()
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
}

// Enabled cho biết đang bật hay không.
func Enabled() bool {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("reg", "query",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", winValueName).CombinedOutput()
		return err == nil && strings.Contains(string(out), winValueName)
	case "darwin":
		p, err := plistPath()
		if err != nil {
			return false
		}
		_, err = os.Stat(p)
		return err == nil
	default:
		p, err := desktopPath()
		if err != nil {
			return false
		}
		_, err = os.Stat(p)
		return err == nil
	}
}

// Location trả về nơi lưu cấu hình autostart (để in ra cho người dùng).
func Location() string {
	switch runtime.GOOS {
	case "windows":
		return `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` + ` \ ` + winValueName
	case "darwin":
		p, _ := plistPath()
		return p
	default:
		p, _ := desktopPath()
		return p
	}
}

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", appID+".plist"), nil
}

func desktopPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", "tuitai.desktop"), nil
}
