// Package limiter cung cấp token bucket giới hạn băng thông dùng chung cho
// toàn bộ tiến trình và cho từng job riêng lẻ.
//
// Limiter là an toàn cho nhiều goroutine. Một Limiter có thể đổi tốc độ khi
// đang chạy (SetRate) để áp dụng cấu hình mới mà không cần tạo lại.
package limiter

import (
	"context"
	"sync"
	"time"
)

// Bước nhỏ nhất để một lần chờ có thể bị Context ngắt; cũng là hạt thời gian
// để tính lại token.
const maxGranularity = 50 * time.Millisecond

// Limiter là token bucket tính theo byte. Limiter nil hoặc có rate <= 0 nghĩa
// là không giới hạn.
type Limiter struct {
	mu     sync.Mutex
	rate   float64 // byte/giây, <= 0 nghĩa là không giới hạn
	burst  float64 // sức chứa tối đa của xô, tính bằng byte
	tokens float64
	last   time.Time
	now    func() time.Time
}

// New tạo Limiter với tốc độ bytesPerSec và sức chứa burst byte. burst <= 0 thì
// được chọn tự động sao cho xô chứa được khoảng 1 giây lưu lượng nhưng không
// nhỏ hơn 32 KiB (để một lần ghi 32 KiB luôn lọt).
func New(bytesPerSec int64, burst int) *Limiter {
	l := &Limiter{now: time.Now}
	l.SetRate(bytesPerSec)
	l.setBurst(float64(burst))
	return l
}

func (l *Limiter) setBurst(b float64) {
	if l == nil {
		return
	}
	if b <= 0 {
		b = l.rate
		if b < 32*1024 {
			b = 32 * 1024
		}
	}
	l.mu.Lock()
	l.burst = b
	if l.tokens > b {
		l.tokens = b
	}
	l.mu.Unlock()
}

// SetRate đổi tốc độ giới hạn; <= 0 tắt giới hạn. An toàn khi gọi đồng thời.
func (l *Limiter) SetRate(bytesPerSec int64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = float64(bytesPerSec)
	l.last = l.now()
	if l.rate > 0 {
		if l.burst <= 0 || l.burst < l.rate {
			l.burst = l.rate
			if l.burst < 32*1024 {
				l.burst = 32 * 1024
			}
		}
		// Cho phép bung ngay một chút sau khi đổi cấu hình.
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
	}
}

// Rate trả về tốc độ hiện tại theo byte/giây (0 nghĩa là không giới hạn).
func (l *Limiter) Rate() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return int64(l.rate)
}

// Limited cho biết Limiter có thực sự chặn hay không.
func (l *Limiter) Limited() bool {
	return l != nil && l.Rate() > 0
}

// WaitN chặn tới khi được phép truyền n byte. Trả về lỗi của ctx nếu bị huỷ.
// n lớn hơn sức chứa xô vẫn hợp lệ: nó được chia thành nhiều lượt.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return ctx.Err()
	}
	remaining := n
	for remaining > 0 {
		l.mu.Lock()
		if l.rate <= 0 {
			l.mu.Unlock()
			return nil
		}
		now := l.now()
		// Nạp token theo thời gian đã trôi qua.
		elapsed := now.Sub(l.last).Seconds()
		if elapsed > 0 {
			l.tokens += elapsed * l.rate
			if l.tokens > l.burst {
				l.tokens = l.burst
			}
			l.last = now
		}
		// Tối đa một lần xô: phần còn lại sẽ được chờ ở vòng sau.
		want := float64(remaining)
		if want > l.burst {
			want = l.burst
		}
		if l.tokens >= want {
			l.tokens -= want
			l.mu.Unlock()
			remaining -= int(want)
			continue
		}
		deficit := want - l.tokens
		wait := time.Duration(deficit / l.rate * float64(time.Second))
		l.mu.Unlock()

		if wait <= 0 {
			wait = time.Millisecond
		}
		if wait > maxGranularity {
			wait = maxGranularity
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return ctx.Err()
}

// Multi áp dụng nhiều Limiter cùng lúc (ví dụ giới hạn toàn cục + giới hạn job).
type Multi struct {
	limiters []*Limiter
}

// NewMulti gộp các limiter; phần tử nil được bỏ qua. Không có limiter nào thì
// Multi cũng không giới hạn.
func NewMulti(ls ...*Limiter) Multi {
	return Multi{limiters: ls}
}

// WaitN chờ lần lượt từng limiter. Chờ toàn cục trước để không giữ token của
// job trong khi còn kẹt ở mức toàn cục.
func (m Multi) WaitN(ctx context.Context, n int) error {
	for _, l := range m.limiters {
		if err := l.WaitN(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// Limited cho biết có limiter nào thực sự chặn.
func (m Multi) Limited() bool {
	for _, l := range m.limiters {
		if l.Limited() {
			return true
		}
	}
	return false
}

// None là Multi không giới hạn, dùng làm giá trị mặc định.
var None = Multi{}
