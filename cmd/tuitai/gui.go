package main

// gui.go — giao diện đồ hoạ kiểu IDM, chạy trong cửa sổ trình duyệt (app mode).
//
// Kiến trúc: một HTTP server nội bộ phục vụ trang HTML/JS nhúng sẵn + REST API
// cho hàng đợi. Cách này giữ được yêu cầu "chỉ thư viện chuẩn" và build được
// cho cả Windows/macOS/Linux mà không cần CGO hay bộ SDK giao diện riêng.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/captajn/tuitai/internal/assets"
	"github.com/captajn/tuitai/internal/queue"
	"github.com/captajn/tuitai/internal/tray"
)

// jobView là dữ liệu một dòng trong bảng tải xuống.
type jobView struct {
	ID       string    `json:"id"`
	URL      string    `json:"url"`
	Dest     string    `json:"dest"`
	State    string    `json:"state"`
	Bytes    int64     `json:"bytes"`
	Total    int64     `json:"total"`
	Speed    int64     `json:"speed"`
	Err      string    `json:"err,omitempty"`
	Threads  int       `json:"threads,omitempty"`
	At       time.Time `json:"at,omitempty"`
	Category string    `json:"category,omitempty"`
}

// guiApp giữ trạng thái riêng của GUI.
type guiApp struct {
	*app
	mu    sync.Mutex
	speed map[string]int64
	errs  map[string]string
}

func (g *guiApp) noteSpeed(id string, v int64, err string) {
	g.mu.Lock()
	if v > 0 {
		g.speed[id] = v
	}
	if err != "" {
		g.errs[id] = err
	}
	g.mu.Unlock()
}

// snapshot gộp job đang chạy (manager) với bản ghi đã lưu (lịch sử).
func (g *guiApp) snapshot() []jobView {
	recs, _ := g.loadRecords()
	byID := make(map[string]queueRecord, len(recs))
	for _, r := range recs {
		byID[r.ID] = r
	}
	out := make([]jobView, 0, len(recs)+8)
	seen := map[string]bool{}
	if g.q != nil {
		for _, j := range g.q.List() {
			b, t := j.Progress()
			v := jobView{
				ID: j.ID, URL: j.URL, Dest: j.Dest, State: string(j.State1()),
				Bytes: b, Total: t, Threads: byID[j.ID].Threads, At: byID[j.ID].At,
			}
			if v.Total == 0 {
				v.Total = byID[j.ID].Total
			}
			g.mu.Lock()
			v.Speed = g.speed[j.ID]
			v.Err = g.errs[j.ID]
			g.mu.Unlock()
			out = append(out, v)
			seen[j.ID] = true
		}
	}
	for _, r := range recs {
		if seen[r.ID] {
			continue
		}
		out = append(out, jobView{
			ID: r.ID, URL: r.URL, Dest: r.Dest, State: r.State,
			Bytes: r.Bytes, Total: r.Total, Threads: r.Threads, At: r.At,
		})
	}
	return out
}

// cmdGUI mở giao diện đồ hoạ.
func (a *app) cmdGUI(args []string) int {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	port := fs.Int("port", 5566, "cổng giao diện (127.0.0.1)")
	noOpen := fs.Bool("no-open", false, "không tự mở cửa sổ")
	useTray := fs.Bool("tray", false, "hiện icon ở khay hệ thống (Windows)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	g := &guiApp{app: a, speed: map[string]int64{}, errs: map[string]string{}}

	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai gui: %v\n", err)
		return exitIO
	}
	a.setup(0, nil)
	stopPump := make(chan struct{})
	go g.pump(stopPump)
	defer close(stopPump)
	for _, r := range recs {
		switch queue.State(r.State) {
		case queue.StateQueued, queue.StatePaused, queue.StateFailed, queue.StateCancelled:
			_ = a.q.Add(a.jobFromRecord(r))
		}
	}
	ctx, cancel := signalContext()
	defer cancel()
	go a.q.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(guiHTML))
	})
	// Ảnh/logo nhúng sẵn — trình duyệt cache 1 ngày.
	artFS := http.StripPrefix("/art/", http.FileServer(http.FS(assets.FS())))
	mux.HandleFunc("/art/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		artFS.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"jobs": g.snapshot()})
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		jobs := g.snapshot()
		var speed int64
		var active, done int
		for _, j := range jobs {
			speed += j.Speed
			switch queue.State(j.State) {
			case queue.StateActive:
				active++
			case queue.StateDone:
				done++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"speed": speed, "active": active, "done": done, "total": len(jobs),
		})
	})
	mux.HandleFunc("/api/add", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URLs     string `json:"urls"`
			Threads  int    `json:"threads"`
			Dir      string `json:"dir"`
			At       string `json:"at"`
			Checksum string `json:"checksum"`
			Mirrors  string `json:"mirrors"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		dir, err := a.cfg.EnsureDownloadDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if strings.TrimSpace(body.Dir) != "" {
			dir = strings.TrimSpace(body.Dir)
		}
		atTime, aerr := parseAt(body.At)
		if aerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": aerr.Error()})
			return
		}
		urls := parseURLList(body.URLs)
		if len(urls) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "không có URL hợp lệ"})
			return
		}
		pending := map[string]bool{}
		var added []string
		for _, u := range urls {
			dest, derr := a.resolveDest(u, dir, "")
			if derr != nil {
				continue
			}
			dest = a.dedupeDest(dest, pending)
			th := body.Threads
			if th <= 0 {
				th = a.cfg.Threads
			}
			rec := queueRecord{
				ID: newID(), URL: u, Dest: dest, Threads: th, At: atTime,
				Checksum: strings.TrimSpace(body.Checksum),
				State:    string(queue.StateQueued), CreatedAt: time.Now(),
			}
			if len(strings.Fields(body.Mirrors)) > 0 {
				rec.Mirrors = strings.Fields(body.Mirrors)
			}
			if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
				continue
			}
			recs, _ := a.loadRecords()
			recs = append(recs, rec)
			_ = a.saveRecords(recs)
			added = append(added, rec.ID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"added": added})
	})
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action string   `json:"action"`
			IDs    []string `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if body.Action == "start" {
			go func() {
				_ = a.runQueueBackground(ctx)
			}()
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		recs, _ := a.loadRecords()
		out := recs[:0]
		drop := map[string]bool{}
		for _, id := range body.IDs {
			switch body.Action {
			case "pause":
				_ = a.q.Pause(id)
			case "resume":
				_ = a.q.Resume(id)
			case "remove":
				_ = a.q.Remove(id)
				drop[id] = true
			}
		}
		for _, rec := range recs {
			if drop[rec.ID] {
				continue
			}
			for _, id := range body.IDs {
				if rec.ID == id {
					switch body.Action {
					case "pause":
						rec.State = "paused"
					case "resume":
						rec.State = "queued"
					}
				}
			}
			out = append(out, rec)
		}
		_ = a.saveRecords(out)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusOK, map[string]any{
				"dir": a.cfg.DownloadDir, "threads": a.cfg.Threads,
				"max_concurrent": a.cfg.MaxConcurrent, "rate_limit": a.cfg.RateLimit,
				"schedule": a.cfg.ScheduleWindow, "organize": a.cfg.OrganizeByType,
				"notify": a.cfg.NotifyOnComplete, "resume": a.cfg.Resume,
			})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		for k, v := range body {
			val := fmt.Sprint(v)
			if b, ok := v.(bool); ok {
				val = fmt.Sprint(b)
			}
			_ = applyConfigKV(a.cfg, k, val)
		}
		if err := a.cfg.Save(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprint(*port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		a.pr.errf("tuitai gui: không mở được %s: %v\n", addr, err)
		return exitNet
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	url := "http://" + addr + "/"
	a.pr.humanf("Tui Download GUI: %s\n", url)
	if !*noOpen {
		openGUIWindow(url)
	}

	// Icon khay + menu (Windows). Không có tray thì vẫn chạy nền bình thường.
	var tr *tray.Tray
	if *useTray {
		if ico, err := assets.Read("tuitai.ico"); err == nil {
			tmp := filepath.Join(os.TempDir(), "tuitai-tray.ico")
			if os.WriteFile(tmp, ico, 0o644) == nil {
				t, terr := tray.Start(tmp, "Tui Tải — đang chạy nền", tray.Callbacks{
					Open: func() { openGUIWindow(url) },
					PauseAll: func() {
						for _, j := range a.q.List() {
							_ = a.q.Pause(j.ID)
						}
						a.recordStates("paused")
					},
					ResumeAll: func() {
						for _, j := range a.q.List() {
							_ = a.q.Resume(j.ID)
						}
						a.recordStates("queued")
					},
					Quit: func() {
						a.q.Close()
						a.syncRecords()
						cancel()
					},
				})
				if terr != nil {
					a.pr.errf("tuitai gui: tray: %v\n", terr)
				} else {
					tr = t
					a.pr.humanf("Đã thu vào khay hệ thống. Nhấp đúp icon để mở lại.\n")
					tr.Notify("Tui Tải đang chạy nền", "Nhấp đúp icon để mở giao diện. Tui vẫn cày đều.")
				}
			}
		}
	}
	if tr != nil {
		defer tr.Stop()
		go watchDone(a, tr)
	}
	go func() { _ = srv.Serve(ln) }()
	<-ctx.Done()
	shutCtx, c2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer c2()
	_ = srv.Shutdown(shutCtx)
	a.q.Close()
	a.syncRecords()
	return exitOK
}

// runQueueBackground chạy hàng đợi không chặn GUI.
func (a *app) runQueueBackground(ctx context.Context) error {
	a.q.Wait()
	return nil
}

// pump chuyển sự kiện tải thành tốc độ/ lỗi cho từng job.
func (g *guiApp) pump(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case ev := <-g.evts:
			var id string
			for _, j := range g.q.List() {
				if j.State1() == queue.StateActive {
					id = j.ID
					break
				}
			}
			if id == "" {
				continue
			}
			msg := ""
			if ev.Err != nil {
				msg = ev.Err.Error()
			}
			g.noteSpeed(id, ev.Speed, msg)
		}
	}
}

// openGUIWindow mở cửa sổ ứng dụng (Chrome/Edge app mode) hoặc trình duyệt mặc định.
func openGUIWindow(url string) {
	switch runtime.GOOS {
	case "windows":
		for _, exe := range []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		} {
			if _, err := exec.LookPath(exe); err == nil {
				_ = exec.Command(exe, "--app="+url, "--window-size=1200,760").Start()
				return
			}
		}
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		if err := exec.Command("xdg-open", url).Start(); err != nil {
			_ = exec.Command("sensible-browser", url).Start()
		}
	}
}

// recordStates đổi trạng thái mọi job trong file queue (dùng cho menu khay).
func (a *app) recordStates(state string) {
	recs, err := a.loadRecords()
	if err != nil {
		return
	}
	for i := range recs {
		recs[i].State = state
	}
	_ = a.saveRecords(recs)
}

// watchDone theo dõi và báo balloon khi có job xong (kiểu IDM).
func watchDone(a *app, tr *tray.Tray) {
	prev := map[string]queue.State{}
	for {
		time.Sleep(2 * time.Second)
		if a.q == nil {
			return
		}
		for _, j := range a.q.List() {
			st := j.State1()
			old, seen := prev[j.ID]
			prev[j.ID] = st
			if !seen || old == st {
				continue
			}
			if st == queue.StateDone {
				tr.Notify("Tải xong: "+filepath.Base(j.Dest), "Xong rồi! Đi rửa mặt đi 🧖")
			}
			if st == queue.StateFailed {
				tr.Notify("Tạch: "+filepath.Base(j.Dest), "Server giận rồi. Thử 'Chạy tiếp' xem sao.")
			}
		}
	}
}
