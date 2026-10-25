// Package schedule giới hạn khung giờ tải (ví dụ chỉ tải 01:00–07:00) và tính
// thời điểm mở cửa kế tiếp.
package schedule

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Daily là khung giờ lặp hằng ngày, tính bằng phút kể từ 00:00 địa phương.
// Start > End nghĩa là khung vắt qua nửa đêm (22:00–06:00).
type Daily struct {
	Start int
	End   int
	// AllDay bật khi không giới hạn.
	AllDay bool
}

// ParseDaily đọc "HH:MM-HH:MM". Chuỗi rỗng nghĩa là mọi lúc.
func ParseDaily(s string) (Daily, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Daily{AllDay: true}, nil
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return Daily{}, fmt.Errorf("schedule: cần dạng HH:MM-HH:MM, nhận %q", s)
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return Daily{}, err
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return Daily{}, err
	}
	if start == end {
		return Daily{}, fmt.Errorf("schedule: giờ bắt đầu và kết thúc trùng nhau (%q)", s)
	}
	return Daily{Start: start, End: end}, nil
}

func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	bits := strings.Split(s, ":")
	if len(bits) != 2 {
		return 0, fmt.Errorf("schedule: giờ không hợp lệ %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(bits[0]))
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("schedule: giờ không hợp lệ %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(bits[1]))
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("schedule: phút không hợp lệ %q", s)
	}
	return h*60 + m, nil
}

// String biểu diễn lại dạng "HH:MM-HH:MM".
func (d Daily) String() string {
	if d.AllDay {
		return "00:00-24:00"
	}
	return fmt.Sprintf("%02d:%02d-%02d:%02d", d.Start/60, d.Start%60, d.End/60, d.End%60)
}

// Active cho biết thời điểm t có nằm trong khung không.
func (d Daily) Active(t time.Time) bool {
	if d.AllDay {
		return true
	}
	cur := t.Hour()*60 + t.Minute()
	if d.Start < d.End {
		return cur >= d.Start && cur < d.End
	}
	// Vắt qua nửa đêm.
	return cur >= d.Start || cur < d.End
}

// NextOpen trả về thời điểm kế tiếp khung mở (t nếu đang mở).
func (d Daily) NextOpen(t time.Time) time.Time {
	if d.AllDay || d.Active(t) {
		return t
	}
	cur := t.Hour()*60 + t.Minute()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if cur < d.Start {
		return day.Add(time.Duration(d.Start) * time.Minute)
	}
	return day.Add(24*time.Hour + time.Duration(d.Start)*time.Minute)
}

// Gate là cổng chờ tới khung giờ cho phép.
type Gate struct {
	Daily Daily
}

// New tạo Gate từ chuỗi cấu hình.
func New(spec string) (*Gate, error) {
	d, err := ParseDaily(spec)
	if err != nil {
		return nil, err
	}
	return &Gate{Daily: d}, nil
}

// Active cho biết hiện tại có được tải hay không.
func (g *Gate) Active(t time.Time) bool {
	if g == nil {
		return true
	}
	return g.Daily.Active(t)
}

// Wait chờ tới khi khung mở, kiểm tra mỗi interval (mặc định 30s).
// Trả về context.Canceled nếu bị huỷ khi đang chờ.
func (g *Gate) Wait(ctx context.Context, now func() time.Time, interval time.Duration) error {
	if g == nil || g.Daily.AllDay {
		return nil
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	for {
		if g.Active(now()) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
