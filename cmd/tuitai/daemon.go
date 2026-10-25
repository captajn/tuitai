package main

// daemon.go: HTTP server nhỏ trên localhost để trình duyệt/extension đẩy link
// sang tuitai (tính năng "browser handoff" của các trình tải xuống desktop).
//
// Bảo vệ: chỉ bind 127.0.0.1, kiểm tra Origin/Referer thuộc về máy cục bộ hoặc
// extension (chrome-extension://, moz-extension://), và tuỳ chọn yêu cầu token.

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/queue"
	"github.com/captajn/tuitai/internal/strutil"
)

func (a *app) cmdDaemon(args []string) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	port := fs.Int("port", 5555, "cổng HTTP (chỉ 127.0.0.1)")
	token := fs.String("token", "", "token bắt buộc trong header X-Tuidl-Token")
	watchClip := fs.Bool("watch-clipboard", false, "tự thêm link khi copy vào clipboard")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet

	a.setup(0, nil)
	ctx, cancel := signalContext()
	defer cancel()
	go a.q.Start(ctx)
	stopPump := make(chan struct{})
	go a.pumpEvents(nil, stopPump)
	defer close(stopPump)

	// Theo dõi clipboard: copy link ở bất kỳ đâu là tự vào hàng đợi.
	if *watchClip {
		seen := map[string]bool{}
		go func() {
			a.pr.errf("tuitai daemon: đang theo dõi clipboard\n")
			_ = clipboardWatch(ctx, 800*time.Millisecond, func(link string) {
				if seen[link] {
					return
				}
				seen[link] = true
				dir, err := a.cfg.EnsureDownloadDir()
				if err != nil {
					return
				}
				if _, err := a.enqueueURL(link, dir, a.cfg.Threads); err != nil {
					a.pr.errf("tuitai daemon (clipboard): %v\n", err)
					return
				}
				a.pr.errf("tuitai daemon: đã thêm từ clipboard: %s\n", link)
			})
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version})
	})
	mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		if !daemonAllowed(r, *token) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin/origin token không hợp lệ"})
			return
		}
		raw := r.URL.Query().Get("url")
		if raw == "" && r.Method == http.MethodPost {
			var body struct {
				URL  string `json:"url"`
				Name string `json:"name"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
			raw, _ = body.URL, body.Name
		}
		if raw == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "thiếu tham số url"})
			return
		}
		dir, err := a.cfg.EnsureDownloadDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		dest, err := a.resolveDest(raw, dir, "")
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		rec := queueRecord{
			ID: newID(), URL: raw, Dest: dest, Threads: a.cfg.Threads,
			State: string(queue.StateQueued), CreatedAt: time.Now(),
		}
		if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		recs, _ := a.loadRecords()
		recs = append(recs, rec)
		_ = a.saveRecords(recs)
		writeJSON(w, http.StatusOK, map[string]any{"id": rec.ID, "dest": rec.Dest})
	})
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		if !daemonAllowed(r, *token) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "không được phép"})
			return
		}
		recs, err := a.loadRecords()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": recs})
	})

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	a.pr.humanf("tuitai daemon: http://%s  (POST /add?url=…, GET /list, GET /health)\n", addr)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			a.pr.errf("tuitai daemon: %v\n", err)
			return exitNet
		}
	case <-ctx.Done():
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	a.q.Wait()
	a.syncRecords()
	return exitOK
}

// daemonAllowed kiểm tra Origin/Referer và token.
func daemonAllowed(r *http.Request, token string) bool {
	if token != "" {
		got := r.Header.Get("X-Tuidl-Token")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if got != token {
			return false
		}
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// Gọi trực tiếp bằng curl/CLI: chấp nhận vì đã bind loopback.
		return true
	}
	if strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://") {
		return true
	}
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]", "::1"} {
		if strings.Contains(origin, "//"+host) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

var _ = strutil.Sanitize
