// Package segments chia một file thành nhiều phần tải song song và cấp phát
// động công việc cho các luồng.
//
// Chia đều một lần là chưa đủ: các luồng có tốc độ khác nhau nên phần của luồng
// chậm sẽ lê thê sau khi phần khác đã xong. Manager vì thế cho phép "steal":
// luồng xong sớm lấy đi nửa đuôi còn lại của segment chậm nhất, và segment đó
// bị thu hẹp lại để đôi bên không ghi trùng vùng.
//
// Mỗi luồng làm việc theo từng Chunk (cửa sổ nhỏ) chứ không nhận trọn segment,
// nhờ vậy việc thu hẹp có hiệu lực gần như tức thì.
package segments

import (
	"errors"
	"fmt"
	"sync"
)

// Giới hạn số segment.
const (
	// DefaultCount là số segment mặc định cho mỗi lần tải.
	DefaultCount = 8
	// MaxCount là số segment tối đa.
	MaxCount = 32
	// MinChunk là kích thước cửa sổ nhỏ nhất khi cấp phát.
	MinChunk = 64 << 10
	// DefaultChunk là kích thước cửa sổ mặc định.
	DefaultChunk = 1 << 20
)

var (
	// ErrEmpty: kích thước file không dương nên không chia được.
	ErrEmpty = errors.New("segments: kích thước file phải dương")
	// ErrDone: đã cấp hết việc.
	ErrDone = errors.New("segments: không còn việc")
)

// Segment là một khoảng byte tuyệt đối, biên đều inclusive.
type Segment struct {
	ID     int   `json:"id"`
	Start  int64 `json:"start"`
	End    int64 `json:"end"`
	Next   int64 `json:"next"` // offset kế tiếp cần tải
	Worker int   `json:"-"`    // luồng đang sở hữu, 0 là chưa có chủ
}

// Len trả về số byte còn lại của segment.
func (s Segment) Len() int64 {
	if s.Next > s.End {
		return 0
	}
	return s.End - s.Next + 1
}

// Done cho biết segment đã cấp hết byte.
func (s Segment) Done() bool { return s.Next > s.End }

// Plan chia [0,size) thành count segment gần bằng nhau và trả về theo thứ tự
// tăng dần. count được kẹp vào 1..MaxCount và không vượt quá size.
func Plan(size int64, count int) ([]Segment, error) {
	if size <= 0 {
		return nil, ErrEmpty
	}
	if count <= 0 {
		count = 1
	}
	if count > MaxCount {
		count = MaxCount
	}
	if int64(count) > size {
		count = int(size)
	}
	base := size / int64(count)
	rem := size % int64(count)

	segs := make([]Segment, 0, count)
	var off int64
	for i := 0; i < count; i++ {
		n := base
		if int64(i) < rem {
			n++
		}
		seg := Segment{ID: i + 1, Start: off, End: off + n - 1, Next: off}
		segs = append(segs, seg)
		off += n
	}
	return segs, nil
}

// FromState dựng lại danh sách segment từ trạng thái đã lưu (đã qua Normalize).
func FromState(segs []Segment) ([]Segment, error) {
	out := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if s.Start < 0 || s.End < s.Start {
			return nil, fmt.Errorf("segments: segment %d không hợp lệ", s.ID)
		}
		if s.Next < s.Start {
			s.Next = s.Start
		}
		if s.Next > s.End+1 {
			s.Next = s.End + 1
		}
		out = append(out, s)
	}
	return out, nil
}

// Chunk là một cửa sổ byte được cấp cho một luồng.
type Chunk struct {
	SegID  int   `json:"seg_id"`
	Start  int64 `json:"start"`
	End    int64 `json:"end"` // inclusive
	Worker int   `json:"worker"`
	Stolen bool  `json:"stolen"`
}

// Len trả về số byte của chunk.
func (c Chunk) Len() int64 { return c.End - c.Start + 1 }

// Range trả về giá trị header "Range" cho chunk.
func (c Chunk) Range() string { return fmt.Sprintf("bytes=%d-%d", c.Start, c.End) }

// Manager cấp phát chunk cho các luồng và điều phối việc steal. An toàn cho
// nhiều goroutine.
type Manager struct {
	mu       sync.Mutex
	segs     []Segment
	byWorker map[int]int // worker -> segment ID đang giữ
	chunk    int64
	minSteal int64
	noSteal  bool
}

// NewManager tạo Manager trên danh sách segment cho trước. chunk <= 0 dùng
// DefaultChunk; minSteal là ngưỡng byte tối thiểu để coi một segment là đáng
// steal (0 dùng 2*chunk).
func NewManager(segs []Segment, chunk, minSteal int64) *Manager {
	if chunk <= 0 {
		chunk = DefaultChunk
	}
	if chunk < MinChunk {
		chunk = MinChunk
	}
	if minSteal <= 0 {
		minSteal = 2 * chunk
	}
	return &Manager{segs: segs, byWorker: make(map[int]int), chunk: chunk, minSteal: minSteal}
}

// DisableSteal tắt cơ chế steal (để kiểm thử hoặc khi muốn cố định phân chia).
func (m *Manager) DisableSteal() { m.noSteal = true }

// Next cấp chunk kế tiếp cho worker. Trả về false khi hết việc và không còn gì
// để steal.
func (m *Manager) Next(worker int) (Chunk, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ưu tiên tiếp tục segment đang giữ.
	if id, ok := m.byWorker[worker]; ok {
		if idx := m.indexOf(id); idx >= 0 {
			s := &m.segs[idx]
			if s.Worker == worker && !s.Done() {
				return m.take(s, worker, false), true
			}
		}
	}
	// Segment chưa có chủ.
	for i := range m.segs {
		s := &m.segs[i]
		if s.Worker == 0 && !s.Done() {
			s.Worker = worker
			m.byWorker[worker] = s.ID
			return m.take(s, worker, false), true
		}
	}
	if m.noSteal {
		return Chunk{}, false
	}
	// Steal nửa đuôi của segment chậm nhất đang do luồng khác giữ.
	var best *Segment
	var bestRem int64 = -1
	for i := range m.segs {
		s := &m.segs[i]
		if s.Worker == 0 || s.Worker == worker || s.Done() {
			continue
		}
		rem := s.Len()
		if rem < m.minSteal {
			continue
		}
		if rem > bestRem {
			best, bestRem = s, rem
		}
	}
	if best == nil {
		return Chunk{}, false
	}
	half := bestRem / 2
	oldEnd := best.End
	best.End = oldEnd - half // nạn nhân bị thu hẹp (best trỏ vào m.segs)
	tail := Segment{
		ID:     len(m.segs) + 1,
		Start:  best.End + 1,
		End:    oldEnd,
		Next:   best.End + 1,
		Worker: worker,
	}
	// ID mới phải không trùng ID cũ.
	for _, s := range m.segs {
		if s.ID >= tail.ID {
			tail.ID = s.ID + 1
		}
	}
	m.segs = append(m.segs, tail)
	m.byWorker[worker] = tail.ID
	return m.take(&m.segs[len(m.segs)-1], worker, true), true
}

// take cắt một cửa sổ tối đa chunk byte từ đầu phần còn lại của segment.
// Phải gọi khi đang giữ m.mu.
func (m *Manager) take(s *Segment, worker int, stolen bool) Chunk {
	start := s.Next
	end := start + m.chunk - 1
	if end > s.End {
		end = s.End
	}
	s.Next = end + 1
	return Chunk{SegID: s.ID, Start: start, End: end, Worker: worker, Stolen: stolen}
}

// indexOf tìm vị trí segment theo ID; -1 nếu không có.
func (m *Manager) indexOf(id int) int {
	for i := range m.segs {
		if m.segs[i].ID == id {
			return i
		}
	}
	return -1
}

// Release giải phóng segment mà worker đang giữ (gọi khi luồng kết thúc).
func (m *Manager) Release(worker int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.byWorker[worker]; ok {
		if idx := m.indexOf(id); idx >= 0 && m.segs[idx].Worker == worker {
			m.segs[idx].Worker = 0
		}
		delete(m.byWorker, worker)
	}
}

// Snapshot trả về bản sao trạng thái segment để ghi xuống đĩa.
func (m *Manager) Snapshot() []Segment {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Segment, 0, len(m.segs))
	for _, s := range m.segs {
		out = append(out, Segment{ID: s.ID, Start: s.Start, End: s.End, Next: s.Next})
	}
	return out
}

// Remaining cộng số byte chưa tải.
func (m *Manager) Remaining() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, s := range m.segs {
		n += s.Len()
	}
	return n
}

// Complete cho biết mọi byte đã được cấp.
func (m *Manager) Complete() bool { return m.Remaining() == 0 }

// Coverage trả về tổng số byte của mọi segment (dùng để kiểm tra toàn vẹn kế hoạch).
func (m *Manager) Coverage() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, s := range m.segs {
		n += s.End - s.Start + 1
	}
	return n
}
