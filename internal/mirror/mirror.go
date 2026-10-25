// Package mirror phân phối các segment tải về cho nhiều URL mirror cùng nội
// dung để tăng thông lượng hoặc né một host chậm.
//
// Các URL được chọn theo vòng tròn trên danh sách đã khử trùng lặp, giữ URL
// gốc ở vị trí đầu tiên.
package mirror

import (
	"net/url"
	"strings"

	"github.com/captajn/tuitai/internal/segments"
)

// Set là danh sách URL đã chuẩn hoá, phần tử đầu là URL gốc.
type Set struct {
	urls []string
}

// New dựng Set từ URL gốc và danh sách mirror. URL rỗng hoặc trùng bị loại;
// URL gốc luôn đứng đầu.
func New(primary string, mirrors []string) *Set {
	s := &Set{}
	seen := make(map[string]bool, len(mirrors)+1)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		key := normalize(u)
		if seen[key] {
			return
		}
		seen[key] = true
		s.urls = append(s.urls, u)
	}
	add(primary)
	for _, m := range mirrors {
		add(m)
	}
	return s
}

// normalize tạo khoá so trùng: bỏ qua scheme, cổng mặc định, fragment và
// chữ hoa host — nhưng vẫn phân biệt path khác nhau.
func normalize(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}
	return host + path + "?" + u.RawQuery
}

// Len trả về số URL phân biệt.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.urls)
}

// Empty cho biết không có URL nào.
func (s *Set) Empty() bool { return s.Len() == 0 }

// All trả về bản sao danh sách URL.
func (s *Set) All() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.urls))
	copy(out, s.urls)
	return out
}

// For trả về URL cho segment thứ index trong tổng số n segment.
func (s *Set) For(index, n int) string {
	if s.Empty() {
		return ""
	}
	if index < 0 {
		index = 0
	}
	return s.urls[index%len(s.urls)]
}

// Next trả về URL kế tiếp sau attempt lần thất bại: xoay vòng để lần thử lại
// rơi vào mirror khác thay vì lặp lại đúng host vừa lỗi.
func (s *Set) Next(current string, attempt int) string {
	if s.Empty() {
		return ""
	}
	start := 0
	for i, u := range s.urls {
		if u == current {
			start = i + 1
			break
		}
	}
	return s.urls[(start+attempt)%len(s.urls)]
}

// Assign gán URL cho từng segment: segment liên tiếp rải đều qua các mirror để
// mỗi host chỉ nhận một phần tải trọng.
func Assign(segs []segments.Segment, primary string, mirrors []string) map[int]string {
	s := New(primary, mirrors)
	out := make(map[int]string, len(segs))
	for i, seg := range segs {
		out[seg.ID] = s.For(i, len(segs))
	}
	return out
}
