// Package downloader tải một file với nhiều kết nối song song, có resume,
// giới hạn tốc độ, retry, mirror và kiểm tra checksum.
//
// Luồng chính của Run:
//  1. Thăm dò URL (probe) để biết kích thước, Range, ETag.
//  2. Nếu resume được và trạng thái cũ còn hợp lệ thì dựng lại segment đã lưu,
//     ngược lại chia mới.
//  3. Mở file tạm `<dest>.part`, preallocate độ dài, rồi cho N worker nhận chunk
//     từ segments.Manager và ghi bằng WriteAt tại offset tuyệt đối.
//  4. Định kỳ ghi trạng thái xuống đĩa để lần chạy sau tiếp tục được.
//  5. Xong: kiểm tra độ dài/checksum rồi đổi tên .part thành tên thật.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/captajn/tuitai/internal/diskfree"
	"github.com/captajn/tuitai/internal/limiter"
	"github.com/captajn/tuitai/internal/mirror"
	"github.com/captajn/tuitai/internal/probe"
	"github.com/captajn/tuitai/internal/segments"
	"github.com/captajn/tuitai/internal/state"
	"github.com/captajn/tuitai/internal/strutil"
	"github.com/captajn/tuitai/internal/verify"
)

// EventType loại sự kiện tiến độ.
type EventType int

const (
	// EventStarted phát khi bắt đầu tải (Bytes/Total đã có).
	EventStarted EventType = iota
	// EventProgress phát định kỳ.
	EventProgress
	// EventRetrying phát khi một chunk phải thử lại.
	EventRetrying
	// EventDone phát khi hoàn tất.
	EventDone
	// EventFailed phát khi thất bại.
	EventFailed
)

func (t EventType) String() string {
	switch t {
	case EventStarted:
		return "started"
	case EventProgress:
		return "progress"
	case EventRetrying:
		return "retrying"
	case EventDone:
		return "done"
	case EventFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Event là một sự kiện tiến độ được phát ra kênh Events.
type Event struct {
	Type  EventType
	Bytes int64         // đã tải
	Total int64         // tổng, -1 nếu không biết
	Speed int64         // byte/giây (trung bình trượt)
	ETA   time.Duration // thời gian còn lại, 0 nếu không ước lượng được
	Err   error
}

// Các lỗi công khai.
var (
	// ErrCancelled: job bị huỷ qua context.
	ErrCancelled = errors.New("downloader: đã huỷ")
	// ErrBadStatus: server trả mã lỗi không mong đợi.
	ErrBadStatus = errors.New("downloader: server trả mã lỗi")
)

// Request mô tả một lần tải.
type Request struct {
	URL       string
	Dest      string // đường dẫn file cuối cùng
	Threads   int    // số kết nối song song (1..32)
	Mirrors   []string
	Header    http.Header
	UserAgent string
	Referer   string
	Username  string
	Password  string
	Retries   int
	Resume    bool
	Checksum  *verify.Spec
}

// Result là kết quả một lần tải thành công.
type Result struct {
	Dest    string
	Bytes   int64
	Total   int64
	Elapsed time.Duration
	Mirrors int // số mirror đã dùng
	Resumed bool
}

// Options cấu hình Downloader.
type Options struct {
	// Global là giới hạn tốc độ dùng chung (nil = không giới hạn).
	Global *limiter.Limiter
	// Events là kênh nhận sự kiện; có thể nil.
	Events chan<- Event
	// StateStore lưu trạng thái resume; nil dùng state.FileStore.
	StateStore state.Store
	// Client là HTTP client; nil dùng http.DefaultClient.
	Client *http.Client
	// ChunkBytes là cửa sổ cấp việc; 0 dùng segments.DefaultChunk.
	ChunkBytes int64
	// SaveEvery là chu kỳ ghi trạng thái; 0 dùng 2s.
	SaveEvery time.Duration
}

// Downloader tải file theo các tuỳ chọn đã cấu hình.
type Downloader struct {
	opts Options
}

// New tạo Downloader.
func New(opts Options) *Downloader {
	if opts.StateStore == nil {
		opts.StateStore = &state.FileStore{}
	}
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.SaveEvery <= 0 {
		opts.SaveEvery = 2 * time.Second
	}
	return &Downloader{opts: opts}
}

func (d *Downloader) emit(ev Event) {
	if d.opts.Events == nil {
		return
	}
	select {
	case d.opts.Events <- ev:
	default: // không chặn tải nếu người nghe chậm
	}
}

// Run tải file theo req. Trả về Result khi xong, hoặc lỗi.
func (d *Downloader) Run(ctx context.Context, req Request) (*Result, error) {
	if req.URL == "" {
		return nil, errors.New("downloader: thiếu URL")
	}
	if req.Dest == "" {
		return nil, errors.New("downloader: thiếu đường dẫn đích")
	}
	if req.Threads <= 0 {
		req.Threads = segments.DefaultCount
	}
	if req.Threads > segments.MaxCount {
		req.Threads = segments.MaxCount
	}
	if req.Retries < 0 {
		req.Retries = 0
	}
	dest := filepath.Clean(req.Dest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("downloader: không tạo được thư mục: %w", err)
	}

	start := time.Now()
	pr := probe.New(d.opts.Client, probe.Options{
		UserAgent:     req.UserAgent,
		Referer:       req.Referer,
		Header:        req.Header,
		Username:      req.Username,
		Password:      req.Password,
		FallbackToGet: true,
	})
	meta, err := pr.Probe(ctx, req.URL)
	if err != nil {
		var unsup probe.ErrUnsupported
		if errors.As(err, &unsup) {
			d.emit(Event{Type: EventFailed, Total: -1, Err: err})
			return nil, err
		}
		return nil, err
	}
	total := meta.Size
	// Kiểm tra dung lượng trống trước khi ghi (bỏ qua nếu không xác định được).
	if total > 0 {
		if err := diskfree.Check(filepath.Dir(dest), total); err != nil {
			d.emit(Event{Type: EventFailed, Total: total, Err: err})
			return nil, err
		}
	}
	d.emit(Event{Type: EventStarted, Total: total})

	if meta.RangeOK && total > 0 && req.Threads > 1 {
		return d.runSegmented(ctx, req, meta, start)
	}
	return d.runSingle(ctx, req, meta, start)
}

// runSingle tải tuần tự (server không hỗ trợ Range).
func (d *Downloader) runSingle(ctx context.Context, req Request, meta *probe.Result, start time.Time) (*Result, error) {
	part := verify.PartPath(req.Dest)
	offset := int64(0)
	if req.Resume {
		if st, err := os.Stat(part); err == nil && st.Size() > 0 && meta.Size > 0 && st.Size() < meta.Size {
			offset = st.Size()
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	httpReq, err := d.newRequest(ctx, req.URL, req, "")
	if err != nil {
		return nil, err
	}
	if offset > 0 && meta.RangeOK {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := d.opts.Client.Do(httpReq)
	if err != nil {
		d.emit(Event{Type: EventFailed, Total: meta.Size, Err: err})
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		err := fmt.Errorf("%w: %s", ErrBadStatus, resp.Status)
		d.emit(Event{Type: EventFailed, Total: meta.Size, Err: err})
		return nil, err
	}
	if offset > 0 && resp.StatusCode != http.StatusPartialContent {
		// Server bỏ qua Range: phải tải lại từ đầu.
		offset = 0
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := f.Truncate(0); err != nil {
			return nil, err
		}
	}

	st := &state.FileStore{}
	if s, ok := d.opts.StateStore.(*state.FileStore); ok {
		st = s
	}
	_ = st.Save(&state.Job{
		ID: req.URL, URL: req.URL, FinalURL: meta.FinalURL, Dest: req.Dest,
		Size: meta.Size, RangeOK: meta.RangeOK, ETag: meta.ETag,
		LastModified: meta.LastModified, MIME: meta.MIME, Filename: meta.Filename,
		Threads: 1, Checksum: req.Checksum,
	})

	var done int64 = offset
	buf := make([]byte, 64<<10)
	lim := d.limiter()
	for {
		if err := ctx.Err(); err != nil {
			return nil, ErrCancelled
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if lim.Limited() {
				if err := lim.WaitN(ctx, n); err != nil {
					return nil, ErrCancelled
				}
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return nil, err
			}
			done += int64(n)
			d.emit(Event{Type: EventProgress, Bytes: done, Total: meta.Size, Speed: speedOf(done-offset, time.Since(start))})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			d.emit(Event{Type: EventFailed, Bytes: done, Total: meta.Size, Err: rerr})
			return nil, rerr
		}
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	// Windows không cho rename file đang mở: phải đóng trước khi công bố.
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := verify.CheckLength(part, meta.Size); err != nil {
		return nil, err
	}
	if err := finalize(req.Dest, part, req.Checksum); err != nil {
		return nil, err
	}
	_ = st.Remove(req.Dest)
	d.emit(Event{Type: EventDone, Bytes: done, Total: meta.Size})
	return &Result{Dest: req.Dest, Bytes: done, Total: meta.Size, Elapsed: time.Since(start), Mirrors: 1}, nil
}

// runSegmented tải song song bằng Range.
func (d *Downloader) runSegmented(ctx context.Context, req Request, meta *probe.Result, start time.Time) (*Result, error) {
	part := verify.PartPath(req.Dest)

	// Dựng segment: ưu tiên trạng thái đã lưu nếu còn hợp lệ.
	var (
		segs    []segments.Segment
		resumed bool
	)
	if req.Resume {
		if saved, err := d.opts.StateStore.Load(req.Dest); err == nil &&
			state.Compatible(saved, meta.Size, meta.ETag, meta.LastModified) && len(saved.Segments) > 0 {
			if fromState, ferr := segments.FromState(toSegments(saved.Segments)); ferr == nil && len(fromState) > 0 {
				segs = fromState
				resumed = true
			}
		}
	}
	if segs == nil {
		planned, perr := segments.Plan(meta.Size, req.Threads)
		if perr != nil {
			return nil, perr
		}
		segs = planned
		_ = os.Remove(part)
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := preallocate(f, meta.Size); err != nil {
		return nil, err
	}

	mgr := segments.NewManager(segs, d.opts.ChunkBytes, 0)
	mirrors := mirror.New(meta.FinalURL, req.Mirrors)

	var (
		done    int64
		failed  atomic.Bool
		errMu   sync.Mutex
		lastErr error
	)
	setErr := func(e error) {
		errMu.Lock()
		if lastErr == nil {
			lastErr = e
		}
		errMu.Unlock()
	}
	for _, s := range mgr.Snapshot() {
		if s.Start < s.Next {
			done += s.Next - s.Start
		}
	}
	d.emit(Event{Type: EventProgress, Bytes: done, Total: meta.Size})

	job := &state.Job{
		ID: req.URL, URL: req.URL, FinalURL: meta.FinalURL, Dest: req.Dest,
		Size: meta.Size, RangeOK: meta.RangeOK, ETag: meta.ETag,
		LastModified: meta.LastModified, MIME: meta.MIME, Filename: meta.Filename,
		Threads: req.Threads, Checksum: req.Checksum,
	}
	saveState := func() {
		job.Segments = toStateSegments(mgr.Snapshot())
		_ = d.opts.StateStore.Save(job)
	}
	saveState()

	// Ticker: báo tiến độ + ghi trạng thái định kỳ.
	stopTicker := make(chan struct{})
	var tickerWG sync.WaitGroup
	tickerWG.Add(1)
	go func() {
		defer tickerWG.Done()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		save := time.NewTicker(d.opts.SaveEvery)
		defer save.Stop()
		var lastBytes int64
		var lastAt = time.Now()
		for {
			select {
			case <-stopTicker:
				return
			case <-tick.C:
				cur := atomic.LoadInt64(&done)
				now := time.Now()
				dt := now.Sub(lastAt).Seconds()
				sp := int64(0)
				if dt > 0 {
					sp = int64(float64(cur-lastBytes) / dt)
				}
				lastBytes, lastAt = cur, now
				eta := time.Duration(0)
				if sp > 0 && meta.Size > cur {
					eta = time.Duration(float64(meta.Size-cur)/float64(sp)) * time.Second
				}
				d.emit(Event{Type: EventProgress, Bytes: cur, Total: meta.Size, Speed: sp, ETA: eta})
			case <-save.C:
				saveState()
			}
		}
	}()

	workers := req.Threads
	if workers > len(mgr.Snapshot()) {
		workers = len(mgr.Snapshot())
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for w := 1; w <= workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer mgr.Release(id)
			for {
				if ctx.Err() != nil {
					setErr(ErrCancelled)
					return
				}
				chunk, ok := mgr.Next(id)
				if !ok {
					return
				}
				if err := d.fetchChunk(ctx, f, req, mirrors, chunk, meta.Size, &done); err != nil {
					failed.Store(true)
					setErr(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stopTicker)
	tickerWG.Wait()

	errMu.Lock()
	finalErr := lastErr
	errMu.Unlock()
	if finalErr != nil {
		saveState()
		d.emit(Event{Type: EventFailed, Bytes: atomic.LoadInt64(&done), Total: meta.Size, Err: finalErr})
		if errors.Is(finalErr, ErrCancelled) {
			return nil, ErrCancelled
		}
		return nil, finalErr
	}
	if !mgr.Complete() {
		saveState()
		err := fmt.Errorf("downloader: còn %d byte chưa tải", mgr.Remaining())
		d.emit(Event{Type: EventFailed, Total: meta.Size, Err: err})
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	// Windows không cho rename file đang mở: phải đóng trước khi công bố.
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := verify.CheckLength(part, meta.Size); err != nil {
		return nil, err
	}
	if err := finalize(req.Dest, part, req.Checksum); err != nil {
		return nil, err
	}
	_ = d.opts.StateStore.Remove(req.Dest)
	total := atomic.LoadInt64(&done)
	d.emit(Event{Type: EventDone, Bytes: total, Total: meta.Size})
	return &Result{
		Dest: req.Dest, Bytes: total, Total: meta.Size,
		Elapsed: time.Since(start), Mirrors: mirrors.Len(), Resumed: resumed,
	}, nil
}

// fetchChunk tải một cửa sổ byte và ghi vào file tại đúng offset.
func (d *Downloader) fetchChunk(ctx context.Context, f *os.File, req Request, mirrors *mirror.Set, chunk segments.Chunk, total int64, done *int64) error {
	attempts := req.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return ErrCancelled
		}
		url := mirrors.For(chunk.SegID, mirrors.Len())
		if attempt > 0 {
			url = mirrors.Next(url, attempt)
			backoff := time.Duration(attempt) * 200 * time.Millisecond
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			d.emit(Event{Type: EventRetrying, Bytes: atomic.LoadInt64(done), Total: total, Err: lastErr})
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ErrCancelled
			}
		}
		err := d.fetchOnce(ctx, f, req, url, chunk, done)
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.Is(err, ErrCancelled) || ctx.Err() != nil {
			return ErrCancelled
		}
	}
	return fmt.Errorf("downloader: chunk %d-%d thất bại sau %d lần: %w", chunk.Start, chunk.End, attempts, lastErr)
}

func (d *Downloader) fetchOnce(ctx context.Context, f *os.File, req Request, url string, chunk segments.Chunk, done *int64) error {
	httpReq, err := d.newRequest(ctx, url, req, chunk.Range())
	if err != nil {
		return err
	}
	resp, err := d.opts.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent, http.StatusOK:
	case http.StatusRequestedRangeNotSatisfiable:
		// Vùng đã có đủ trên đĩa (thường do resume): coi như xong.
		atomic.AddInt64(done, chunk.Len())
		return nil
	default:
		return fmt.Errorf("%w: %s cho %s", ErrBadStatus, resp.Status, url)
	}

	buf := make([]byte, 64<<10)
	var off = chunk.Start
	remaining := chunk.Len()
	lim := d.limiter()
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return ErrCancelled
		}
		want := int64(len(buf))
		if remaining < want {
			want = remaining
		}
		n, rerr := resp.Body.Read(buf[:want])
		if n > 0 {
			if lim.Limited() {
				if err := lim.WaitN(ctx, n); err != nil {
					return ErrCancelled
				}
			}
			if _, werr := f.WriteAt(buf[:n], off); werr != nil {
				return werr
			}
			off += int64(n)
			remaining -= int64(n)
			atomic.AddInt64(done, int64(n))
		}
		if rerr == io.EOF {
			if remaining > 0 {
				return fmt.Errorf("downloader: kết nối đóng sớm, còn %d byte", remaining)
			}
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

func (d *Downloader) newRequest(ctx context.Context, url string, req Request, rangeHeader string) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if req.UserAgent != "" {
		httpReq.Header.Set("User-Agent", req.UserAgent)
	}
	if req.Referer != "" {
		httpReq.Header.Set("Referer", req.Referer)
	}
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Set(k, v)
		}
	}
	if rangeHeader != "" {
		httpReq.Header.Set("Range", rangeHeader)
	}
	if req.Username != "" || req.Password != "" {
		httpReq.SetBasicAuth(req.Username, req.Password)
	}
	return httpReq, nil
}

func (d *Downloader) limiter() limiter.Multi {
	if d.opts.Global == nil {
		return limiter.None
	}
	return limiter.NewMulti(d.opts.Global)
}

func preallocate(f *os.File, size int64) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() == size {
		return nil
	}
	return f.Truncate(size)
}

func finalize(dest, part string, spec *verify.Spec) error {
	if err := verify.Verify(part, -1, spec); err != nil {
		return err
	}
	return verify.Finalize(part, dest)
}

func toSegments(in []state.Segment) []segments.Segment {
	out := make([]segments.Segment, 0, len(in))
	for _, s := range in {
		out = append(out, segments.Segment{ID: s.ID, Start: s.Start, End: s.End, Next: s.Next})
	}
	return out
}

func toStateSegments(in []segments.Segment) []state.Segment {
	out := make([]state.Segment, 0, len(in))
	for _, s := range in {
		out = append(out, state.Segment{ID: s.ID, Start: s.Start, End: s.End, Next: s.Next})
	}
	return out
}

func speedOf(bytes int64, d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(float64(bytes) / d.Seconds())
}

var _ = strutil.Bytes // giữ import cho tài liệu/ tiện dụng ở tầng trên
