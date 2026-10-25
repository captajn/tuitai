package main

// commands.go: hiện thực các lệnh add / run / list / status / pause / resume /
// rm / probe / config.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/category"
	"github.com/captajn/tuitai/internal/config"
	"github.com/captajn/tuitai/internal/downloader"
	"github.com/captajn/tuitai/internal/notify"
	"github.com/captajn/tuitai/internal/probe"
	"github.com/captajn/tuitai/internal/queue"
	"github.com/captajn/tuitai/internal/strutil"
	"github.com/captajn/tuitai/internal/verify"
)

// stringList là flag lặp lại được (--mirror a --mirror b).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// cmdAdd thêm job rồi (mặc định) chạy ngay.
func (a *app) cmdAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	dir := fs.String("o", "", "thư mục lưu")
	fs.StringVar(dir, "dir", "", "thư mục lưu")
	name := fs.String("n", "", "tên file")
	fs.StringVar(name, "name", "", "tên file")
	threads := fs.Int("t", 0, "số kết nối song song")
	fs.IntVar(threads, "threads", 0, "số kết nối song song")
	priority := fs.Int("priority", 0, "mức ưu tiên")
	referer := fs.String("referer", "", "header Referer")
	user := fs.String("user", "", "user cho Basic auth")
	pass := fs.String("pass", "", "mật khẩu cho Basic auth")
	checksum := fs.String("checksum", "", "sha256:<hex> hoặc md5:<hex>")
	noRun := fs.Bool("no-run", false, "chỉ xếp hàng, không chạy")
	atStr := fs.String("at", "", "hẹn giờ tải (HH:MM hoặc RFC3339)")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	var mirrors, headers stringList
	fs.Var(&mirrors, "mirror", "mirror (lặp lại được)")
	fs.Var(&headers, "header", `header dạng "K: V" (lặp lại được)`)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet

	urls := fs.Args()
	if len(urls) == 0 {
		a.pr.errf("tuitai add: cần ít nhất một URL\n")
		return exitUsage
	}
	if *checksum != "" {
		if _, err := verify.ParseSpec(*checksum); err != nil {
			a.pr.errf("tuitai add: --checksum không hợp lệ: %v\n", err)
			return exitUsage
		}
	}
	saveDir, err := a.cfg.EnsureDownloadDir()
	if err != nil {
		a.pr.errf("tuitai add: không tạo được thư mục tải: %v\n", err)
		return exitIO
	}
	if *dir != "" {
		saveDir = *dir
		if err := os.MkdirAll(saveDir, 0o755); err != nil {
			a.pr.errf("tuitai add: thư mục %q không dùng được: %v\n", saveDir, err)
			return exitIO
		}
	}
	threadsN := *threads
	if threadsN <= 0 {
		threadsN = a.cfg.Threads
	}
	if threadsN > config.MaxThreads {
		threadsN = config.MaxThreads
	}

	atTime, aerr := parseAt(*atStr)
	if aerr != nil {
		a.pr.errf("tuitai add: %v\n", aerr)
		return exitUsage
	}
	a.setup(0, nil)
	var added []queueRecord
	pending := map[string]bool{}
	for _, raw := range urls {
		dest, derr := a.resolveDest(raw, saveDir, *name)
		if derr != nil {
			a.pr.errf("tuitai add: %v\n", derr)
			return exitNet
		}
		dest = a.dedupeDest(dest, pending)
		rec := queueRecord{
			ID: newID(), URL: raw, Dest: dest, Priority: *priority,
			Threads: threadsN, Mirrors: mirrors, Header: headers,
			Referer: *referer, User: *user, Pass: *pass, Checksum: *checksum,
			At: atTime, State: string(queue.StateQueued), CreatedAt: time.Now(),
		}
		added = append(added, rec)
		if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
			a.pr.errf("tuitai add: %v\n", err)
			return exitConflict
		}
	}
	recs, _ := a.loadRecords()
	recs = append(recs, added...)
	if err := a.saveRecords(recs); err != nil {
		a.pr.errf("tuitai add: không ghi được hàng đợi: %v\n", err)
		return exitIO
	}

	if a.pr.asJSON {
		_ = a.pr.okJSON("add", added)
	} else {
		for _, r := range added {
			a.pr.humanf("Đã thêm %s → %s\n", r.URL, r.Dest)
		}
	}
	if *noRun {
		return exitOK
	}
	return a.runQueue(context.Background(), nil)
}

// resolveDest chọn đường dẫn đích: ưu tiên --name, rồi tên file từ server,
// rồi tên suy ra từ URL; cuối cùng tránh ghi đè file đã có.
func (a *app) resolveDest(rawURL, dir, name string) (string, error) {
	base := strutil.FilenameFromURL(rawURL)
	mime := ""
	if name == "" {
		pr := probe.New(mustClient(a.cfg), probe.Options{UserAgent: a.cfg.UserAgent, Referer: a.cfg.Referer, FallbackToGet: true})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Một lần probe lấy cả tên file lẫn MIME (MIME dùng để phân loại).
		if res, err := pr.Probe(ctx, rawURL); err == nil {
			if res.Filename != "" {
				base = res.Filename
			}
			mime = res.MIME
		}
	}
	if name != "" {
		base = name
	}
	if base == "" {
		base = "download.bin"
	}
	safe := strutil.Sanitize(base)
	// Phân loại vào thư mục con (Video/Music/Documents/…) nếu bật.
	cl := category.New(dir, a.cfg.OrganizeByType, a.cfg.CategoryDirs)
	target := cl.Dir(safe, mime)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	return strutil.UniquePath(target, safe), nil
}

// dedupeDest tránh hai job ghi vào cùng một file: nếu đích đã có trong hàng đợi,
// đang chờ trong lô hiện tại (pending) hoặc đã tồn tại trên đĩa thì thêm " (n)".
func (a *app) dedupeDest(dest string, pending map[string]bool) string {
	norm := func(p string) string { return strings.ToLower(filepath.Clean(p)) }
	used := map[string]bool{}
	if recs, err := a.loadRecords(); err == nil {
		for _, r := range recs {
			used[norm(r.Dest)] = true
		}
	}
	for k := range pending {
		used[k] = true
	}
	if !used[norm(dest)] {
		if pending != nil {
			pending[norm(dest)] = true
		}
		return dest
	}
	ext := filepath.Ext(dest)
	base := strings.TrimSuffix(dest, ext)
	for i := 2; i < 1000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if used[norm(cand)] {
			continue
		}
		if _, err := os.Stat(cand); err == nil {
			continue
		}
		if pending != nil {
			pending[norm(cand)] = true
		}
		return cand
	}
	return dest
}

// cmdRun chạy hàng đợi ở tiền cảnh.
func (a *app) cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	return a.runQueue(context.Background(), nil)
}

// runQueue nạp các job đang chờ/đang dở rồi chạy tới khi xong.
// onEvent (có thể nil) nhận tiến độ để tầng TUI/daemon hiển thị.
func (a *app) runQueue(ctx context.Context, onEvent func(*queue.Job, downloader.Event)) int {
	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	maxConc := a.cfg.MaxConcurrent
	a.setup(maxConc, onEvent)

	// Kênh sự kiện: chuyển tiếp tới onEvent hoặc in ra màn hình.
	stopPump := make(chan struct{})
	go a.pumpEvents(onEvent, stopPump)

	queued := 0
	for _, r := range recs {
		switch queue.State(r.State) {
		case queue.StateQueued, queue.StatePaused, queue.StateFailed, queue.StateCancelled:
		default:
			continue
		}
		j := a.jobFromRecord(r)
		if err := a.q.Add(j); err != nil {
			continue
		}
		queued++
	}
	if queued == 0 {
		close(stopPump)
		a.pr.errf("tuitai: hàng đợi trống\n")
		return exitOK
	}

	ctx, cancel := signalContext()
	defer cancel()
	go a.q.Start(ctx)
	a.q.Wait()
	close(stopPump)
	a.syncRecords()

	// Mã thoát: 0 nếu mọi job xong, 1 nếu có job lỗi.
	for _, j := range a.q.List() {
		if j.State1() == queue.StateFailed {
			a.pr.errf("tuitai: job %s lỗi: %v\n", j.ID, errOf(j))
			return exitNet
		}
	}
	return exitOK
}

func errOf(j *queue.Job) error {
	jl := j
	return jl.Err
}

// pumpEvents in tiến độ ra stdout khi không có onEvent, và cập nhật record.
func (a *app) pumpEvents(onEvent func(*queue.Job, downloader.Event), stop <-chan struct{}) {
	last := map[string]time.Time{}
	for {
		select {
		case <-stop:
			return
		case ev := <-a.evts:
			// Tìm job khớp theo tiến độ (kênh sự kiện dùng chung).
			var job *queue.Job
			for _, j := range a.q.List() {
				if j.State1() == queue.StateActive {
					job = j
					break
				}
			}
			if job != nil {
				job.SetProgress(ev.Bytes, ev.Total)
			}
			if onEvent != nil {
				onEvent(job, ev)
				continue
			}
			if a.pr.asJSON || a.pr.quiet || job == nil {
				continue
			}
			if ev.Type == downloader.EventRetrying {
				a.pr.humanf("[%s] thử lại: %v\n", job.ID, ev.Err)
				continue
			}
			if ev.Type == downloader.EventDone {
				a.pr.humanf("\r[%s] xong %s%s\n", job.ID, humanBytes(ev.Bytes), "                    ")
				if a.cfg.NotifyOnComplete {
					_ = notify.Notify(notify.Config{Enabled: true, Command: a.cfg.NotifyCommand},
						"Tui Download", "Xong: "+fileName(job.Dest))
				}
				continue
			}
			now := time.Now()
			if t, ok := last[job.ID]; ok && now.Sub(t) < 200*time.Millisecond {
				continue
			}
			last[job.ID] = now
			frac, known := percent(ev.Bytes, ev.Total)
			line := fmt.Sprintf("\r[%s] %s %s/%s %s", job.ID, bar(frac, 20),
				humanBytes(ev.Bytes), humanBytes(ev.Total), humanRate(float64(ev.Speed)))
			if known {
				line += " " + fmt.Sprintf("%.1f%%", frac*100)
			}
			if ev.ETA > 0 {
				line += " còn " + humanETA(ev.ETA)
			}
			a.pr.humanf("%s   ", line)
		}
	}
}

// cmdList in bảng job.
func (a *app) cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	search := fs.String("search", "", "lọc theo URL/tên file/ID")
	fs.StringVar(search, "s", "", "lọc theo URL/tên file/ID")
	sortBy := fs.String("sort", "", "sắp xếp: id|state|size|progress|date")
	csvOut := fs.String("csv", "", "xuất CSV ra file")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	recs = filterRecords(recs, *search)
	sortRecords(recs, *sortBy)
	if *csvOut != "" {
		if err := writeCSV(*csvOut, recs); err != nil {
			a.pr.errf("tuitai list: %v\n", err)
			return exitIO
		}
		a.pr.humanf("Đã ghi %d dòng vào %s\n", len(recs), *csvOut)
		return exitOK
	}
	if a.pr.asJSON {
		_ = a.pr.okJSON("list", recs)
		return exitOK
	}
	if len(recs) == 0 {
		a.pr.humanf("Hàng đợi trống.\n")
		return exitOK
	}
	rows := make([][]string, 0, len(recs))
	for _, r := range recs {
		pct := "-"
		if r.Total > 0 {
			pct = fmt.Sprintf("%.1f%%", float64(r.Bytes)/float64(r.Total)*100)
		}
		rows = append(rows, []string{
			r.ID, r.State, pct, humanBytes(r.Total), filepath.Base(r.Dest), truncateURL(r.URL, 48),
		})
	}
	a.pr.table([]string{"ID", "TRẠNG THÁI", "TIẾN ĐỘ", "KÍCH THƯỚC", "FILE", "URL"}, rows)
	return exitOK
}

// cmdStatus xem chi tiết.
func (a *app) cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON = *jsonOut
	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	id := ""
	if fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		// Không có ID: in tổng quan.
		counts := map[string]int{}
		for _, r := range recs {
			counts[r.State]++
		}
		if a.pr.asJSON {
			_ = a.pr.okJSON("status", counts)
			return exitOK
		}
		a.pr.humanf("Tổng job: %d\n", len(recs))
		for _, st := range []string{"queued", "active", "paused", "done", "failed", "cancelled"} {
			if counts[st] > 0 {
				a.pr.humanf("  %-10s %d\n", st, counts[st])
			}
		}
		return exitOK
	}
	for _, r := range recs {
		if r.ID != id {
			continue
		}
		if a.pr.asJSON {
			_ = a.pr.okJSON("status", r)
			return exitOK
		}
		a.pr.humanf("ID        : %s\n", r.ID)
		a.pr.humanf("Trạng thái: %s\n", r.State)
		a.pr.humanf("URL       : %s\n", r.URL)
		a.pr.humanf("Đích      : %s\n", r.Dest)
		a.pr.humanf("Đã tải    : %s / %s\n", humanBytes(r.Bytes), humanBytes(r.Total))
		a.pr.humanf("Luồng     : %d\n", r.Threads)
		if r.Checksum != "" {
			a.pr.humanf("Checksum  : %s\n", r.Checksum)
		}
		return exitOK
	}
	a.pr.errf("tuitai: không có job %q\n", id)
	return exitNotFound
}

// cmdSimple xử lý pause/resume/rm.
func (a *app) cmdSimple(args []string, action string) int {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON = *jsonOut
	ids := fs.Args()
	if len(ids) == 0 {
		a.pr.errf("tuitai %s: cần ID job\n", action)
		return exitUsage
	}
	recs, err := a.loadRecords()
	if err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	byID := make(map[string]*queueRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}
	code := exitOK
	for _, id := range ids {
		r, ok := byID[id]
		if !ok {
			a.pr.errf("tuitai: không có job %q\n", id)
			code = exitNotFound
			continue
		}
		switch action {
		case "pause":
			if r.State == "active" {
				r.State = "paused"
			} else if r.State == "queued" {
				r.State = "paused"
			} else {
				a.pr.errf("tuitai pause: job %s đang %s, không tạm dừng được\n", id, r.State)
				code = exitConflict
				continue
			}
		case "resume":
			if r.State != "paused" && r.State != "failed" && r.State != "cancelled" {
				a.pr.errf("tuitai resume: job %s đang %s, không tiếp tục được\n", id, r.State)
				code = exitConflict
				continue
			}
			r.State = "queued"
		case "rm":
			delete(byID, id)
		}
		a.pr.humanf("%s: %s\n", action, id)
	}
	if action == "rm" {
		out := recs[:0]
		for _, r := range recs {
			if _, keep := byID[r.ID]; keep {
				out = append(out, r)
			}
		}
		recs = out
	}
	if err := a.saveRecords(recs); err != nil {
		a.pr.errf("tuitai: %v\n", err)
		return exitIO
	}
	if action == "rm" {
		for _, id := range ids {
			if r, ok := byID[id]; ok {
				_ = os.Remove(r.Dest + ".tuitai")
			}
		}
	}
	if a.pr.asJSON {
		_ = a.pr.okJSON(action, ids)
	}
	return code
}

// cmdProbe in metadata của URL.
func (a *app) cmdProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	if fs.NArg() == 0 {
		a.pr.errf("tuitai probe: cần URL\n")
		return exitUsage
	}
	pr := probe.New(mustClient(a.cfg), probe.Options{
		UserAgent: a.cfg.UserAgent, Referer: a.cfg.Referer, FallbackToGet: true,
	})
	for _, u := range fs.Args() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		res, err := pr.Probe(ctx, u)
		cancel()
		if err != nil {
			a.pr.errf("tuitai probe: %v\n", err)
			return exitNet
		}
		if a.pr.asJSON {
			_ = a.pr.okJSON("probe", res)
			continue
		}
		a.pr.humanf("URL          : %s\n", res.URL)
		if res.FinalURL != res.URL {
			a.pr.humanf("Sau redirect : %s\n", res.FinalURL)
		}
		size := "không rõ"
		if res.Size >= 0 {
			size = fmt.Sprintf("%s (%d byte)", humanBytes(res.Size), res.Size)
		}
		a.pr.humanf("Kích thước   : %s\n", size)
		a.pr.humanf("Hỗ trợ Range : %v\n", res.RangeOK)
		a.pr.humanf("Tên file     : %s\n", res.Filename)
		a.pr.humanf("MIME         : %s\n", res.MIME)
		if res.ETag != "" {
			a.pr.humanf("ETag         : %s\n", res.ETag)
		}
	}
	return exitOK
}

// cmdConfig xem/ghi cấu hình.
func (a *app) cmdConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	var sets stringList
	fs.Var(&sets, "set", "k=v (lặp lại được)")
	path := fs.Bool("path", false, "in đường dẫn file cấu hình")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON = *jsonOut
	if *path {
		p, err := config.Path()
		if err != nil {
			a.pr.errf("tuitai: %v\n", err)
			return exitIO
		}
		a.pr.humanf("%s\n", p)
		return exitOK
	}
	for _, kv := range sets {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			a.pr.errf("tuitai config: --set cần dạng k=v, nhận %q\n", kv)
			return exitUsage
		}
		if err := applyConfigKV(a.cfg, kv[:i], kv[i+1:]); err != nil {
			a.pr.errf("tuitai config: %v\n", err)
			return exitUsage
		}
	}
	if len(sets) > 0 {
		if err := a.cfg.Validate(); err != nil {
			a.pr.errf("tuitai config: %v\n", err)
			return exitUsage
		}
		if err := a.cfg.Save(); err != nil {
			a.pr.errf("tuitai config: %v\n", err)
			return exitIO
		}
	}
	if a.pr.asJSON {
		_ = a.pr.okJSON("config", a.cfg)
		return exitOK
	}
	a.pr.humanf("%s\n", a.cfg.Summary())
	return exitOK
}

func truncateURL(u string, n int) string {
	if len(u) <= n {
		return u
	}
	return u[:n-1] + "…"
}
