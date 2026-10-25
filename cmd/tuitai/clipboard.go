package main

// clipboard.go: đọc clipboard đa nền tảng và nhặt URL http(s) trong đó.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// clipboardTool trả về lệnh đọc clipboard theo hệ điều hành hiện tại.
func clipboardTool() (string, []string, bool) {
	switch runtime.GOOS {
	case "windows":
		// Ưu tiên PowerShell có sẵn; Get-Clipboard chỉ có trên Windows.
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, []string{"-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard"}, true
		}
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, []string{"-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard"}, true
		}
	case "darwin":
		if p, err := exec.LookPath("pbpaste"); err == nil {
			return p, nil, true
		}
	case "linux":
		if p, err := exec.LookPath("wl-paste"); err == nil {
			return p, []string{"--no-newline"}, true
		}
		if p, err := exec.LookPath("xclip"); err == nil {
			return p, []string{"-selection", "clipboard", "-o"}, true
		}
		if p, err := exec.LookPath("xsel"); err == nil {
			return p, []string{"--clipboard", "--output"}, true
		}
	}
	return "", nil, false
}

// readClipboard đọc nội dung clipboard; lỗi khi không có công cụ.
func readClipboard() (string, error) {
	name, args, ok := clipboardTool()
	if !ok {
		return "", errors.New("không tìm thấy công cụ clipboard cho " + runtime.GOOS)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// extractURLs tách các URL http(s) khỏi đoạn text.
func extractURLs(s string) []string {
	raw := urlPattern.FindAllString(s, -1)
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, u := range raw {
		u = strings.TrimRight(u, ".,;:)]}")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// clipboardWatch theo dõi clipboard và gọi onLink cho mỗi URL mới.
// Chạy tới khi ctx xong.
func clipboardWatch(ctx context.Context, interval time.Duration, onLink func(string)) error {
	if interval <= 0 {
		interval = time.Second
	}
	last := ""
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		text, err := readClipboard()
		if err != nil || text == last {
			continue
		}
		last = text
		for _, u := range extractURLs(text) {
			onLink(u)
		}
	}
}
