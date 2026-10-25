// Package strutil cung cấp tiện ích chuỗi để tạo tên file an toàn trên mọi
// nền tảng (Windows/macOS/Linux) và để hiển thị tiến độ cho con người.
package strutil

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// MaxNameBytes là độ dài tối đa (theo byte) của một tên file sau khi làm sạch.
// Giá trị này nhỏ hơn 255 để chừa chỗ cho phần mở rộng khi filesystem giới hạn
// tên thành phần ở 255 byte.
const MaxNameBytes = 200

// fallbackName được dùng khi tên đầu vào rỗng hoặc chỉ toàn ký tự bị loại bỏ.
const fallbackName = "download"

// forbidden là các ký tự không hợp lệ trên Windows và cũng gây rắc rối trên
// shell POSIX; dấu phân cách thư mục được thay bằng '_' để không tạo cây con.
const forbidden = `<>:"/\|?*`

// reservedNames là tên thiết bị của DOS mà Windows từ chối dùng làm tên file.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// Sanitize biến một tên tuỳ ý thành tên file an toàn, một thành phần, không
// rỗng và không vượt quá MaxNameBytes. Dùng cho cả tên do server gợi ý.
func Sanitize(name string) string {
	name = baseName(name)

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			// ký tự điều khiển: bỏ hẳn
		case unicode.IsSpace(r) && (r == '\t' || r == '\n' || r == '\r'):
			b.WriteByte(' ')
		case strings.ContainsRune(forbidden, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}

	out := strings.TrimSpace(b.String())
	// Windows cắt bỏ dấu chấm/khoảng trắng ở cuối nên tự cắt trước cho khớp.
	out = strings.Trim(out, " .")
	// ".." và "." không phải tên file dùng được.
	if out == "" || out == "." || out == ".." {
		out = fallbackName
	}
	// Tên thiết bị DOS bị chặn kể cả khi có phần mở rộng ("CON.txt").
	if reservedNames[strings.ToUpper(strings.TrimSuffix(out, filepath.Ext(out)))] {
		out = "f_" + out
	}
	return truncate(out, MaxNameBytes)
}

// baseName lấy thành phần cuối sau cả hai loại dấu phân cách, đồng thời cắt
// query/fragment còn sót lại.
func baseName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	// Đi ngược để không cấp phát slice trung gian.
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' || name[i] == '\\' {
			name = name[i+1:]
			break
		}
	}
	return name
}

// truncate cắt chuỗi còn tối đa max byte, giữ nguyên phần mở rộng nếu được và
// luôn cắt ở ranh giới rune để không tạo UTF-8 hỏng.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := filepath.Ext(s)
	if len(ext) > max/2 {
		// phần mở rộng quá dài: cắt cả nó
		ext = ""
	}
	base := s[:len(s)-len(ext)]
	limit := max - len(ext)
	for len(base) > limit {
		_, size := lastRune(base)
		base = base[:len(base)-size]
	}
	base = strings.TrimRight(base, " .")
	if base == "" {
		base = fallbackName
	}
	return base + ext
}

func lastRune(s string) (rune, int) {
	rs := []rune(s)
	if len(rs) == 0 {
		return 0, 1
	}
	r := rs[len(rs)-1]
	return r, len(string(r))
}

// UniquePath trả về một đường dẫn chưa tồn tại trong dir, bắt đầu bằng name và
// thêm hậu tố "-1", "-2"... nếu cần. name đã phải được Sanitize trước.
func UniquePath(dir, name string) string {
	name = Sanitize(name)
	p := filepath.Join(dir, name)
	if !exists(p) {
		return p
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// Chừa chỗ cho hậu tố "-999".
	stem = truncate(stem, MaxNameBytes-8)
	for i := 1; i < 100000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if !exists(cand) {
			return cand
		}
	}
	// Rơi xuống đây coi như đã cạn; dùng timestamp để vẫn không ghi đè.
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

// FilenameFromURL suy ra tên file gợi ý từ URL (đã Sanitize); trả về fallbackName
// khi URL không có thành phần hữu ích.
func FilenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fallbackName
	}
	name := Sanitize(u.Path)
	return name
}

// Bytes định dạng số byte theo đơn vị nhị phân, ví dụ "1.5 MiB".
func Bytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Speed định dạng tốc độ theo byte/giây, ví dụ "2.1 MiB/s".
func Speed(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "0 B/s"
	}
	return Bytes(int64(bytesPerSecond)) + "/s"
}

// Duration định dạng thời lượng còn lại ngắn gọn ("12s", "3m10s", "1h02m").
func Duration(d time.Duration) string {
	if d <= 0 {
		return "--"
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Round(time.Second)/time.Second)) + "s"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// Percent tính phần trăm hoàn thành (0..100), an toàn với total <= 0.
func Percent(done, total int64) float64 {
	if total <= 0 {
		return 0
	}
	p := float64(done) / float64(total) * 100
	if p > 100 {
		return 100
	}
	if p < 0 {
		return 0
	}
	return p
}
