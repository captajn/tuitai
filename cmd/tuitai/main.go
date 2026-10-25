package main

// main.go: điểm vào của `tuitai` — bản Go đa nền tảng lấy cảm hứng từ tính năng
// của app "Tui Download" (macOS). Toàn bộ phụ thuộc là thư viện chuẩn.
//
// Các lệnh:
//
//	tuitai add <url...>      thêm vào hàng đợi (mặc định chạy ngay)
//	tuitai run               chạy hàng đợi ở tiền cảnh
//	tuitai list | status     xem hàng đợi
//	tuitai pause|resume|rm   điều khiển job
//	tuitai tui               giao diện terminal
//	tuitai daemon            HTTP server nhận link từ trình duyệt
//	tuitai probe <url>       xem metadata trước khi tải
//	tuitai config            xem/ghi cấu hình

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/captajn/tuitai/internal/config"
	"github.com/captajn/tuitai/internal/downloader"
	"github.com/captajn/tuitai/internal/limiter"
	"github.com/captajn/tuitai/internal/queue"
	"github.com/captajn/tuitai/internal/schedule"
	"github.com/captajn/tuitai/internal/state"
	"github.com/captajn/tuitai/internal/verify"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// app gom mọi thứ một lệnh cần.
type app struct {
	cfg  *config.Config
	pr   *printer
	lim  *limiter.Limiter
	dl   *downloader.Downloader
	q    *queue.Manager
	gate *schedule.Gate
	mu   sync.Mutex
	evts chan downloader.Event
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return exitOK
	}
	cmd := args[0]
	rest := args[1:]

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuitai: không đọc được cấu hình: %v\n", err)
		return exitIO
	}
	a := &app{cfg: cfg, pr: newPrinter()}

	switch cmd {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return exitOK
	case "version", "--version", "-v":
		fmt.Printf("tuitai %s (%s/%s)\n", version, osName(), archName())
		return exitOK
	case "add":
		return a.cmdAdd(rest)
	case "run":
		return a.cmdRun(rest)
	case "list", "ls":
		return a.cmdList(rest)
	case "status":
		return a.cmdStatus(rest)
	case "pause":
		return a.cmdSimple(rest, "pause")
	case "resume":
		return a.cmdSimple(rest, "resume")
	case "rm", "remove":
		return a.cmdSimple(rest, "rm")
	case "tui":
		return a.cmdTUI(rest)
	case "daemon", "serve":
		return a.cmdDaemon(rest)
	case "probe":
		return a.cmdProbe(rest)
	case "grab":
		return a.cmdGrab(rest)
	case "hls":
		return a.cmdHLS(rest)
	case "gui", "ui", "window":
		return a.cmdGUI(rest)
	case "autostart":
		return a.cmdAutostart(rest)
	case "extension", "ext":
		return a.cmdExtension(rest)
	case "doctor":
		return a.cmdDoctor(rest)
	case "import":
		return a.cmdImport(rest)
	case "config":
		return a.cmdConfig(rest)
	default:
		fmt.Fprintf(os.Stderr, "tuitai: lệnh không hợp lệ %q\n\n", cmd)
		usage(os.Stderr)
		return exitUsage
	}
}

var version = "0.0.0-dev" // ghi đè khi build: -ldflags "-X main.version=vX.Y.Z"

func usage(w *os.File) {
	fmt.Fprint(w, `tuitai — trình tải xuống đa kết nối, đa nền tảng (Go)

CÁCH DÙNG
  tuitai <lệnh> [tuỳ chọn]

TẢI XUỐNG
  add <url...>          thêm job rồi chạy ngay (--no-run để chỉ xếp hàng)
      -o, --dir DIR     thư mục lưu (mặc định theo cấu hình)
      -n, --name NAME   tên file lưu
      -t, --threads N   số kết nối song song (1..32)
      --mirror URL      thêm mirror (lặp lại được)
      --header "K: V"   thêm header (lặp lại được)
      --referer URL     header Referer
      --user U --pass P Basic auth
      --checksum SPEC   "sha256:<hex>" | "md5:<hex>"
      --priority N      ưu tiên (số lớn chạy trước)
  run                   chạy toàn bộ hàng đợi ở tiền cảnh
  grab <url trang>      tìm link file/media trên trang rồi tải
      --ext mp4,m3u8    chỉ lấy các loại này
      --pages           lấy cả link trang
      --no-run          chỉ liệt kê, không tải
  import <file|->       nhập danh sách URL (mỗi dòng một URL, '#' là chú thích)
  hls <m3u8>            tải playlist HLS rồi ghép thành một file
  gui                   mở giao diện đồ hoạ (cửa sổ kiểu IDM)
      --tray            chạy nền + icon ở khay hệ thống (Windows)
      --no-open         không tự mở cửa sổ
      --port N          đổi cổng (mặc định 5566)
  tui                   giao diện terminal (bảng job, phím tắt)
  daemon                HTTP server 127.0.0.1 nhận link từ extension

QUẢN LÝ
  list                  liệt kê job
  status [id]           chi tiết một job
  pause|resume <id...>  tạm dừng / tiếp tục
  rm <id...>            xoá khỏi hàng đợi
  probe <url>           xem kích thước, Range, ETag, tên file

KHÁC
  config [--set k=v]    xem/ghi cấu hình
      organize=1|0      phân loại file vào thư mục con
      schedule=01:00-07:00   khung giờ được tải
  extension export      xuất extension Chrome MV3 ra đĩa (Load unpacked)
  autostart on|off      bật/tắt chạy cùng hệ điều hành (kèm tray)
  doctor                kiểm tra môi trường
  version | help

Tuỳ chọn chung: --json (máy đọc), --quiet.
`)
}

func osName() string   { return runtime.GOOS }
func archName() string { return runtime.GOARCH }

// ---------------------------------------------------------------- hạ tầng

// setup dựng limiter, downloader và queue từ cấu hình hiện tại.
func (a *app) setup(maxConcurrent int, onEvent func(*queue.Job, downloader.Event)) *queue.Manager {
	rate := a.cfg.RateLimit
	if a.cfg.ScheduleWindow != "" && a.cfg.OffPeakRate > 0 {
		if g, err := schedule.New(a.cfg.ScheduleWindow); err == nil && !g.Active(time.Now()) {
			rate = a.cfg.OffPeakRate
		}
	}
	if rate > 0 {
		a.lim = limiter.New(rate, 0)
	}
	a.evts = make(chan downloader.Event, 256)
	a.dl = downloader.New(downloader.Options{
		Global:     a.lim,
		Events:     a.evts,
		StateStore: &state.FileStore{},
		Client:     mustClient(a.cfg),
		ChunkBytes: a.cfg.ChunkBytes,
	})
	if maxConcurrent <= 0 {
		maxConcurrent = a.cfg.MaxConcurrent
	}
	a.q = queue.New(queue.Options{
		MaxConcurrent: maxConcurrent,
		OnEvent:       onEvent,
	})
	// Khung giờ tải: rỗng nghĩa là mọi lúc.
	if g, err := schedule.New(a.cfg.ScheduleWindow); err == nil {
		a.gate = g
	}
	return a.q
}

func mustClient(c *config.Config) *http.Client {
	cl, err := c.NewClient()
	if err != nil {
		return http.DefaultClient
	}
	return cl
}

// jobFromRecord dựng lại queue.Job từ bản ghi đã lưu.
func (a *app) jobFromRecord(r queueRecord) *queue.Job {
	j := &queue.Job{
		ID:        r.ID,
		URL:       r.URL,
		Dest:      r.Dest,
		Priority:  r.Priority,
		CreatedAt: r.CreatedAt,
	}
	j.Run = func(ctx context.Context, job *queue.Job) error {
		// Job hẹn giờ: chờ tới thời điểm --at.
		if !r.At.IsZero() && time.Now().Before(r.At) {
			d := time.Until(r.At)
			a.pr.errf("tuitai: job %s hẹn %s (còn %s)\n", job.ID, r.At.Format("15:04 02/01"), humanETA(d))
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
		}
		// Chờ tới khung giờ cho phép (nếu có cấu hình schedule_window).
		if a.gate != nil && !a.gate.Active(time.Now()) {
			a.pr.errf("tuitai: ngoài khung giờ tải (%s), job %s đang chờ tới %s\n",
				a.cfg.ScheduleWindow, job.ID, a.gate.Daily.NextOpen(time.Now()).Format("15:04 02/01"))
			if err := a.gate.Wait(ctx, time.Now, 30*time.Second); err != nil {
				return err
			}
		}
		res, err := a.dl.Run(ctx, downloader.Request{
			URL:       job.URL,
			Dest:      job.Dest,
			Threads:   r.Threads,
			Mirrors:   r.Mirrors,
			Header:    headerFromStrings(r.Header),
			UserAgent: a.cfg.UserAgent,
			Referer:   r.Referer,
			Username:  r.User,
			Password:  r.Pass,
			Retries:   a.cfg.Retries,
			Resume:    a.cfg.Resume,
			Checksum:  parseSpec(r.Checksum),
		})
		if err != nil {
			return err
		}
		job.SetProgress(res.Bytes, res.Total)
		return nil
	}
	return j
}

// queueRecord là dạng lưu bền của một job (không chứa hàm Run).
type queueRecord struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Dest      string    `json:"dest"`
	Priority  int       `json:"priority,omitempty"`
	Threads   int       `json:"threads,omitempty"`
	Mirrors   []string  `json:"mirrors,omitempty"`
	Header    []string  `json:"header,omitempty"`
	Referer   string    `json:"referer,omitempty"`
	User      string    `json:"user,omitempty"`
	Pass      string    `json:"pass,omitempty"`
	Checksum  string    `json:"checksum,omitempty"`
	At        time.Time `json:"at,omitempty"`
	State     string    `json:"state"`
	Bytes     int64     `json:"bytes,omitempty"`
	Total     int64     `json:"total,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *app) recordsPath() (string, error) {
	p, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "queue.json"), nil
}

func (a *app) loadRecords() ([]queueRecord, error) {
	p, err := a.recordsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var recs []queueRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("queue.json hỏng: %w", err)
	}
	return recs, nil
}

func (a *app) saveRecords(recs []queueRecord) error {
	p, err := a.recordsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// syncRecords ghi lại trạng thái các job hiện có trong manager.
func (a *app) syncRecords() {
	if a.q == nil {
		return
	}
	recs, _ := a.loadRecords()
	byID := make(map[string]*queueRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	for _, j := range a.q.List() {
		r, ok := byID[j.ID]
		if !ok {
			continue
		}
		r.State = string(j.State1())
		b, t := j.Progress()
		if b > 0 {
			r.Bytes = b
		}
		if t > 0 {
			r.Total = t
		}
	}
	_ = a.saveRecords(recs)
}

// signalContext trả về context huỷ khi Ctrl+C.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}

func headerFromStrings(in []string) http.Header {
	if len(in) == 0 {
		return nil
	}
	h := make(http.Header, len(in))
	for _, kv := range in {
		if i := strings.IndexByte(kv, ':'); i > 0 {
			h.Add(strings.TrimSpace(kv[:i]), strings.TrimSpace(kv[i+1:]))
		}
	}
	return h
}

func parseSpec(s string) *verify.Spec {
	if s == "" {
		return nil
	}
	sp, err := verify.ParseSpec(s)
	if err != nil {
		return nil
	}
	return sp
}

func newID() string {
	return "j" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
