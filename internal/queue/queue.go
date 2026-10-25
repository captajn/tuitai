// Package queue quản lý hàng đợi job: trạng thái, mức ưu tiên và số job chạy
// đồng thời. Manager không biết cách tải — mỗi Job mang theo hàm Run do tầng
// trên cung cấp (thường là lời gọi downloader.Run).
package queue

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/captajn/tuitai/internal/downloader"
)

// State là trạng thái của một job.
type State string

const (
	StateQueued    State = "queued"
	StateActive    State = "active"
	StatePaused    State = "paused"
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Lỗi công khai.
var (
	ErrNotFound = errors.New("queue: không có job này")
	ErrClosed   = errors.New("queue: đã đóng")
)

// Job là một mục trong hàng đợi.
type Job struct {
	ID        string
	URL       string
	Dest      string
	Priority  int // số lớn được chạy trước
	State     State
	Bytes     int64
	Total     int64
	Err       error
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
	Run       func(ctx context.Context, j *Job) error

	mu     sync.Mutex
	cancel context.CancelFunc
}

// SetProgress cập nhật tiến độ an toàn.
func (j *Job) SetProgress(bytes, total int64) {
	j.mu.Lock()
	j.Bytes, j.Total = bytes, total
	j.mu.Unlock()
}

// Progress đọc tiến độ an toàn.
func (j *Job) Progress() (int64, int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Bytes, j.Total
}

// State1 đọc trạng thái an toàn.
func (j *Job) State1() State {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State
}

func (j *Job) setState(s State) {
	j.mu.Lock()
	j.State = s
	j.mu.Unlock()
}

// Options cấu hình Manager.
type Options struct {
	// MaxConcurrent là số job chạy đồng thời (mặc định 3).
	MaxConcurrent int
	// OnEvent nhận sự kiện tiến độ kèm job tương ứng (có thể nil).
	OnEvent func(j *Job, ev downloader.Event)
}

// Manager điều phối các job.
type Manager struct {
	opts Options

	mu      sync.Mutex
	jobs    map[string]*Job
	order   []*Job
	running map[string]context.CancelFunc
	sem     chan struct{}
	closed  bool
	wg      sync.WaitGroup
	wake    chan struct{}
}

// New tạo Manager.
func New(opts Options) *Manager {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 3
	}
	return &Manager{
		opts:    opts,
		jobs:    make(map[string]*Job),
		running: make(map[string]context.CancelFunc),
		sem:     make(chan struct{}, opts.MaxConcurrent),
		wake:    make(chan struct{}, 1),
	}
}

// Add đưa job vào hàng đợi.
func (m *Manager) Add(j *Job) error {
	if j == nil {
		return errors.New("queue: job rỗng")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if j.ID == "" {
		j.ID = fmt.Sprintf("job-%d", len(m.jobs)+1)
	}
	if _, dup := m.jobs[j.ID]; dup {
		return fmt.Errorf("queue: job %q đã tồn tại", j.ID)
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now()
	}
	j.State = StateQueued
	m.jobs[j.ID] = j
	m.order = append(m.order, j)
	m.sortLocked()
	m.poke()
	return nil
}

// List trả về bản sao danh sách job theo thứ tự ưu tiên/khởi tạo.
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, len(m.order))
	copy(out, m.order)
	return out
}

// Get tìm job theo ID.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// Pause dừng một job đang chạy hoặc đang chờ.
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	cancel := m.running[id]
	st := j.State1()
	m.mu.Unlock()

	switch st {
	case StateActive:
		if cancel != nil {
			cancel()
		}
		j.setState(StatePaused)
	case StateQueued:
		j.setState(StatePaused)
	default:
		return fmt.Errorf("queue: không tạm dừng được job ở trạng thái %s", st)
	}
	return nil
}

// Resume đưa job đã tạm dừng trở lại hàng đợi.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	st := j.State1()
	m.mu.Unlock()
	if st != StatePaused && st != StateFailed && st != StateCancelled {
		return fmt.Errorf("queue: không tiếp tục được job ở trạng thái %s", st)
	}
	j.setState(StateQueued)
	j.mu.Lock()
	j.Err = nil
	j.mu.Unlock()
	m.poke()
	return nil
}

// Cancel huỷ job và loại khỏi hàng đợi.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	cancel := m.running[id]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	j.setState(StateCancelled)
	return nil
}

// Remove xoá job khỏi hàng đợi (huỷ nếu đang chạy).
func (m *Manager) Remove(id string) error {
	if err := m.Cancel(id); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	out := m.order[:0]
	for _, j := range m.order {
		if j.ID != id {
			out = append(out, j)
		}
	}
	m.order = out
	return nil
}

// Start chạy vòng điều phối cho tới khi ctx bị huỷ hoặc Close được gọi.
func (m *Manager) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			m.dispatch(ctx)
		}
	}
}

func (m *Manager) dispatch(ctx context.Context) {
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		var next *Job
		for _, j := range m.order {
			if j.State1() == StateQueued {
				if _, busy := m.running[j.ID]; !busy {
					next = j
					break
				}
			}
		}
		m.mu.Unlock()
		if next == nil {
			return
		}
		select {
		case m.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		m.launch(ctx, next)
	}
}

func (m *Manager) launch(parent context.Context, j *Job) {
	ctx, cancel := context.WithCancel(parent)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		<-m.sem
		return
	}
	m.running[j.ID] = cancel
	j.setState(StateActive)
	j.mu.Lock()
	j.StartedAt = time.Now()
	j.mu.Unlock()
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.running, j.ID)
			m.mu.Unlock()
			<-m.sem
			m.poke() // có slot trống: thử chạy job kế tiếp
		}()
		var err error
		if j.Run != nil {
			err = j.Run(ctx, j)
		}
		j.mu.Lock()
		j.EndedAt = time.Now()
		if err != nil {
			switch j.State {
			case StatePaused, StateCancelled:
				// giữ nguyên trạng thái do người dùng đặt
			default:
				j.State = StateFailed
				j.Err = err
			}
		} else {
			j.State = StateDone
			j.Err = nil
		}
		j.mu.Unlock()
		if m.opts.OnEvent != nil {
			if err != nil {
				m.opts.OnEvent(j, downloader.Event{Type: downloader.EventFailed, Err: err})
			} else {
				m.opts.OnEvent(j, downloader.Event{Type: downloader.EventDone})
			}
		}
	}()
}

// Wait chờ tới khi không còn job đang chạy hoặc đang chờ.
func (m *Manager) Wait() {
	for {
		m.mu.Lock()
		pending := 0
		for _, j := range m.jobs {
			switch j.State1() {
			case StateQueued, StateActive:
				pending++
			}
		}
		running := len(m.running)
		m.mu.Unlock()
		if pending == 0 && running == 0 {
			m.wg.Wait()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Close dừng nhận job mới và huỷ các job đang chạy.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	cancels := make([]context.CancelFunc, 0, len(m.running))
	for _, c := range m.running {
		cancels = append(cancels, c)
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	m.wg.Wait()
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// sortLocked sắp xếp theo Priority giảm dần, rồi theo thời điểm tạo tăng dần.
func (m *Manager) sortLocked() {
	sort.SliceStable(m.order, func(i, j int) bool {
		if m.order[i].Priority != m.order[j].Priority {
			return m.order[i].Priority > m.order[j].Priority
		}
		return m.order[i].CreatedAt.Before(m.order[j].CreatedAt)
	})
}
