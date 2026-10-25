package main

// output.go: chế độ xuất --json / --quiet và định dạng số dạng người đọc.

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mã thoát.
const (
	exitOK       = 0
	exitErr      = 1
	exitUsage    = 2
	exitNotFound = 3
	exitConflict = 4
	exitIO       = 5
	exitNet      = 6
)

// printer gom stdout/stderr cùng 2 cờ toàn cục.
type printer struct {
	out    io.Writer
	errw   io.Writer
	asJSON bool
	quiet  bool
}

func newPrinter() *printer {
	return &printer{out: os.Stdout, errw: os.Stderr}
}

// humanf in ra stdout khi không bật --json và không --quiet.
func (p *printer) humanf(format string, a ...any) {
	if p.asJSON || p.quiet {
		return
	}
	fmt.Fprintf(p.out, format, a...)
}

// line in một dòng trống dẫn xuất; bỏ qua ở chế độ json/quiet.
func (p *printer) line() {
	p.humanf("\n")
}

// table in bảng chỉ khi là người đọc thường.
func (p *printer) table(header []string, rows [][]string) {
	if p.asJSON || p.quiet {
		return
	}
	writeTable(p.out, header, rows)
}

// errf in lỗi ra stderr; vẫn in khi --quiet nhưng không in khi --json
// (JSON có trường "error" riêng).
func (p *printer) errf(format string, a ...any) {
	if p.asJSON {
		return
	}
	fmt.Fprintf(p.errw, format, a...)
}

// emitJSON ghi một đối tượng JSON gọn cho stdout.
func (p *printer) emitJSON(v any) error {
	enc := json.NewEncoder(p.out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// result là phong bì JSON thống nhất cho mọi lệnh.
type result struct {
	OK     bool   `json:"ok"`
	Cmd    string `json:"cmd"`
	Error  string `json:"error,omitempty"`
	Detail any    `json:"detail,omitempty"`
}

func (p *printer) okJSON(cmd string, detail any) error {
	return p.emitJSON(result{OK: true, Cmd: cmd, Detail: detail})
}

func (p *printer) errJSON(cmd string, err error) error {
	return p.emitJSON(result{OK: false, Cmd: cmd, Error: err.Error()})
}

// writeTable vẽ bảng canh trái, tự tính độ rộng cột.
func writeTable(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = displayWidth(h)
	}
	for _, r := range rows {
		for i := range header {
			if i < len(r) {
				if n := displayWidth(r[i]); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}
	emit := func(cells []string) {
		var b strings.Builder
		for i := range widths {
			c := ""
			if i < len(cells) {
				c = cells[i]
			}
			b.WriteString(c)
			if i < len(widths)-1 {
				if pad := widths[i] - displayWidth(c) + 2; pad > 0 {
					b.WriteString(strings.Repeat(" ", pad))
				}
			}
		}
		b.WriteString("\n")
		io.WriteString(w, b.String())
	}
	emit(header)
	sep := make([]string, len(widths))
	for i, n := range widths {
		sep[i] = strings.Repeat("-", n)
	}
	emit(sep)
	for _, r := range rows {
		emit(r)
	}
}

// displayWidth đếm rune để canh cột tiếng Việt tương đối ổn.
func displayWidth(s string) int {
	return len([]rune(s))
}

// humanBytes định dạng dung lượng: B, KiB, MiB, GiB, TiB.
func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return scaleBytes(float64(n), []string{"KiB", "MiB", "GiB", "TiB", "PiB"})
}

// humanRate định dạng tốc độ (byte/giây) kèm hậu tố /s.
func humanRate(bps float64) string {
	if bps < 1 {
		return "0 B/s"
	}
	if bps < 1024 {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	return scaleBytes(bps, []string{"KiB/s", "MiB/s", "GiB/s", "TiB/s", "PiB/s"})
}

func scaleBytes(v float64, units []string) string {
	i := -1
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f %s", v, units[i])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, units[i])
	default:
		return fmt.Sprintf("%.2f %s", v, units[i])
	}
}

// humanETA định dạng thời gian còn lại; 0 hoặc âm là không xác định.
func humanETA(d time.Duration) string {
	if d <= 0 || d > 100*24*time.Hour {
		return "-"
	}
	secs := int64(math.Round(d.Seconds()))
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	case secs < 86400:
		return fmt.Sprintf("%dh%02dm", secs/3600, (secs%3600)/60)
	default:
		return fmt.Sprintf("%dd%02dh", secs/86400, (secs%86400)/3600)
	}
}

// percent trả về 0..100; tổng chưa biết thì 0 và false.
func percent(done, total int64) (float64, bool) {
	if total <= 0 {
		return 0, false
	}
	p := float64(done) / float64(total) * 100
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p, true
}

// bar vẽ thanh tiến độ dài w ô.
func bar(frac float64, w int) string {
	if w <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full := int(math.Round(frac * float64(w)))
	if full > w {
		full = w
	}
	return strings.Repeat("#", full) + strings.Repeat("-", w-full)
}

// envInt đọc biến môi trường dạng số nguyên dương.
func envInt(name string) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// orDefault trả về v nếu dương, ngược lại def.
func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
