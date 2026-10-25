package main

// tui.go: giao diện terminal thuần ANSI (không phụ thuộc thư viện ngoài).
//
// Bố cục: một bảng job + dòng trạng thái + dòng trợ giúp phím. Vẽ lại mỗi
// 250ms bằng cách đưa con trỏ về góc trên trái rồi ghi đè từng dòng, nên không
// nhấp nháy như khi xoá toàn màn hình.
//
// Phím: ↑/k, ↓/j chọn · p tạm dừng · r tiếp tục · d xoá · a thêm URL · q thoát.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/captajn/tuitai/internal/downloader"
	"github.com/captajn/tuitai/internal/queue"
)

// tuiState là dữ liệu dùng chung giữa vòng vẽ và vòng đọc phím.
type tuiState struct {
	mu      sync.Mutex
	speed   map[string]int64
	errText string
	sel     int
}

func (a *app) cmdTUI(args []string) int {
	restore, err := termSetup()
	if err != nil {
		a.pr.errf("tuitai tui: không đặt được chế độ terminal: %v\n", err)
		return exitIO
	}
	defer restore()

	st := &tuiState{speed: map[string]int64{}}
	ctx, cancel := signalContext()
	defer cancel()

	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	a.setup(0, func(j *queue.Job, ev downloader.Event) {
		st.mu.Lock()
		if j != nil {
			j.SetProgress(ev.Bytes, ev.Total)
			if ev.Speed > 0 {
				st.speed[j.ID] = ev.Speed
			}
			if ev.Err != nil {
				st.errText = ev.Err.Error()
			}
			if ev.Type == downloader.EventDone {
				st.speed[j.ID] = 0
			}
		}
		st.mu.Unlock()
	})
	stopPump := make(chan struct{})
	go a.pumpEvents(func(j *queue.Job, ev downloader.Event) {
		st.mu.Lock()
		if j != nil {
			j.SetProgress(ev.Bytes, ev.Total)
			if ev.Speed > 0 {
				st.speed[j.ID] = ev.Speed
			}
			if ev.Err != nil {
				st.errText = ev.Err.Error()
			}
		}
		st.mu.Unlock()
	}, stopPump)
	defer close(stopPump)

	for _, r := range recs {
		switch queue.State(r.State) {
		case queue.StateQueued, queue.StatePaused, queue.StateFailed, queue.StateCancelled:
			if err := a.q.Add(a.jobFromRecord(r)); err != nil {
				continue
			}
		}
	}
	go a.q.Start(ctx)

	keys := make(chan byte, 16)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				close(keys)
				return
			}
			select {
			case keys <- buf[0]:
			default:
			}
		}
	}()

	fmt.Print("\033[?25l") // ẩn con trỏ
	defer fmt.Print("\033[?25h\033[2J\033[H")

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	quit := false
	for !quit {
		a.renderTUI(st)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			quit = true
		case b, ok := <-keys:
			if !ok {
				quit = true
				break
			}
			switch b {
			case 'q', 'Q', 3: // 3 = Ctrl+C
				quit = true
			case 'j':
				st.moveSelection(a, +1)
			case 'k':
				st.moveSelection(a, -1)
			case 'p', 'P':
				if id := st.selectedID(a); id != "" {
					_ = a.q.Pause(id)
					st.note(a, "đã tạm dừng "+id)
				}
			case 'r', 'R':
				if id := st.selectedID(a); id != "" {
					_ = a.q.Resume(id)
					st.note(a, "tiếp tục "+id)
				}
			case 'd', 'D':
				if id := st.selectedID(a); id != "" {
					_ = a.q.Remove(id)
					if recs, err := a.loadRecords(); err == nil {
						out := recs[:0]
						for _, r := range recs {
							if r.ID != id {
								out = append(out, r)
							}
						}
						_ = a.saveRecords(out)
					}
					st.note(a, "đã xoá "+id)
				}
			case 'a', 'A':
				url := a.promptURL(restore)
				restore, _ = termSetup()
				if url != "" {
					a.tuiAdd(url, st)
				}
			case 27: // ESC [ A/B
				seq := make([]byte, 2)
				if n, _ := os.Stdin.Read(seq); n == 2 && seq[0] == '[' {
					switch seq[1] {
					case 'A':
						st.moveSelection(a, -1)
					case 'B':
						st.moveSelection(a, +1)
					}
				}
			}
		}
	}

	cancel()
	a.q.Close()
	a.syncRecords()
	fmt.Print("\033[2J\033[H")
	return exitOK
}

// note ghi thông báo tạm thời ở dòng trạng thái.
func (s *tuiState) note(a *app, msg string) {
	s.mu.Lock()
	s.errText = msg
	s.mu.Unlock()
}

func (s *tuiState) selectedID(a *app) string {
	jobs := a.sortedJobs()
	if len(jobs) == 0 {
		return ""
	}
	s.mu.Lock()
	sel := s.sel
	s.mu.Unlock()
	if sel >= len(jobs) {
		sel = len(jobs) - 1
	}
	if sel < 0 {
		return ""
	}
	return jobs[sel].ID
}

func (s *tuiState) moveSelection(a *app, delta int) {
	n := len(a.sortedJobs())
	if n == 0 {
		return
	}
	s.mu.Lock()
	s.sel += delta
	if s.sel < 0 {
		s.sel = 0
	}
	if s.sel >= n {
		s.sel = n - 1
	}
	s.mu.Unlock()
}

// sortedJobs trả về job theo thứ tự hiển thị ổn định.
func (a *app) sortedJobs() []*queue.Job {
	jobs := a.q.List()
	sort.SliceStable(jobs, func(i, j int) bool {
		ri, rj := rankOf(jobs[i].State1()), rankOf(jobs[j].State1())
		if ri != rj {
			return ri < rj
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs
}

func rankOf(s queue.State) int {
	switch s {
	case queue.StateActive:
		return 0
	case queue.StateQueued:
		return 1
	case queue.StatePaused:
		return 2
	case queue.StateFailed:
		return 3
	case queue.StateCancelled:
		return 4
	default:
		return 5
	}
}

// renderTUI vẽ lại toàn bộ khung hình tại chỗ.
func (a *app) renderTUI(st *tuiState) {
	const (
		bold  = "\033[1m"
		dim   = "\033[2m"
		reset = "\033[0m"
	)
	width := termWidth()
	jobs := a.sortedJobs()

	var b strings.Builder
	b.WriteString("\033[H")
	b.WriteString(bold + "tuitai — trình tải xuống đa kết nối" + reset + "\n")
	b.WriteString(dim + strings.Repeat("─", min(width, 110)) + reset + "\n")

	st.mu.Lock()
	sel := st.sel
	speed := make(map[string]int64, len(st.speed))
	for k, v := range st.speed {
		speed[k] = v
	}
	msg := st.errText
	st.mu.Unlock()

	if len(jobs) == 0 {
		b.WriteString(dim + "Chưa có job nào. Nhấn 'a' để thêm URL, 'q' để thoát." + reset + "\n")
	} else {
		header := fmt.Sprintf("%-2s %-10s %-22s %10s %10s %-8s %s",
			"", "ID", "TIẾN ĐỘ", "ĐÃ TẢI", "TỐC ĐỘ", "CÒN", "FILE")
		b.WriteString(dim + header + reset + "\n")
		maxRows := len(jobs)
		if maxRows > 14 {
			maxRows = 14
		}
		for i := 0; i < maxRows; i++ {
			j := jobs[i]
			done, total := j.Progress()
			frac, _ := percent(done, total)
			status := string(j.State1())
			eta := "-"
			if sp := speed[j.ID]; sp > 0 && total > done {
				eta = humanETA(time.Duration(float64(total-done)/float64(sp)) * time.Second)
			}
			marker := "  "
			if i == sel {
				marker = "▶ "
			}
			b.WriteString(fmt.Sprintf("%s%-10s %-22s %10s %10s %-8s %s\n",
				marker, j.ID, bar(frac, 20), humanBytes(done), humanRate(float64(speed[j.ID])), eta,
				truncateURL(fileName(j.Dest), 40)))
			b.WriteString(fmt.Sprintf("   %s%s%s\n", dim, status, reset))
			_ = status
		}
		if len(jobs) > maxRows {
			b.WriteString(dim + fmt.Sprintf("… và %d job nữa\n", len(jobs)-maxRows) + reset)
		}
	}

	b.WriteString(dim + strings.Repeat("─", min(width, 110)) + reset + "\n")
	if msg != "" {
		b.WriteString("  " + msg + "\n")
	}
	b.WriteString("  " + dim + "a thêm · p dừng · r tiếp tục · d xoá · ↑/↓ chọn · q thoát" + reset + "\n")
	b.WriteString("\033[J") // xoá phần còn lại
	fmt.Print(b.String())
}

// promptURL tạm rời raw mode để người dùng nhập URL.
func (a *app) promptURL(restore func()) string {
	restore()
	fmt.Print("\033[2J\033[H Nhập URL (Enter để trống, bỏ qua): ")
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}
		if buf[0] == '\r' || buf[0] == '\n' {
			break
		}
		if buf[0] == 27 {
			return ""
		}
		fmt.Print(string(buf[0]))
		sb.WriteByte(buf[0])
	}
	fmt.Print("\n")
	return strings.TrimSpace(sb.String())
}

// tuiAdd thêm URL ngay trong TUI.
func (a *app) tuiAdd(rawURL string, st *tuiState) {
	dir, err := a.cfg.EnsureDownloadDir()
	if err != nil {
		st.note(a, "lỗi thư mục: "+err.Error())
		return
	}
	dest, err := a.resolveDest(rawURL, dir, "")
	if err != nil {
		st.note(a, "lỗi URL: "+err.Error())
		return
	}
	rec := queueRecord{
		ID: newID(), URL: rawURL, Dest: dest, Threads: a.cfg.Threads,
		State: string(queue.StateQueued), CreatedAt: time.Now(),
	}
	if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
		st.note(a, "không thêm được: "+err.Error())
		return
	}
	recs, _ := a.loadRecords()
	recs = append(recs, rec)
	_ = a.saveRecords(recs)
	st.note(a, "đã thêm "+fileName(dest))
}

func fileName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
