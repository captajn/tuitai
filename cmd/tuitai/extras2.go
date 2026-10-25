package main

// extras2.go: lệnh HLS, doctor, CSV và các tiện ích thời gian.

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/autostart"
	"github.com/captajn/tuitai/internal/config"
	"github.com/captajn/tuitai/internal/diskfree"
	"github.com/captajn/tuitai/internal/extension"
	"github.com/captajn/tuitai/internal/hls"
	"github.com/captajn/tuitai/internal/limiter"
	"github.com/captajn/tuitai/internal/notify"
)

// parseAt hiểu "HH:MM" (lần kế tiếp trong ngày) hoặc RFC3339.
func parseAt(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	bits := strings.Split(s, ":")
	if len(bits) != 2 {
		return time.Time{}, fmt.Errorf("--at cần HH:MM hoặc RFC3339, nhận %q", s)
	}
	h, err1 := strconv.Atoi(bits[0])
	m, err2 := strconv.Atoi(bits[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return time.Time{}, fmt.Errorf("--at không hợp lệ: %q", s)
	}
	now := time.Now()
	at := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !at.After(now) {
		at = at.Add(24 * time.Hour)
	}
	return at, nil
}

// cmdHLS tải playlist .m3u8 rồi ghép thành một file.
func (a *app) cmdHLS(args []string) int {
	fs := flag.NewFlagSet("hls", flag.ContinueOnError)
	dir := fs.String("o", "", "thư mục lưu")
	name := fs.String("n", "", "tên file đầu ra")
	threads := fs.Int("t", 0, "số segment tải song song")
	quiet := fs.Bool("quiet", false, "im lặng")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	if fs.NArg() == 0 {
		a.pr.errf("tuitai hls: cần URL playlist .m3u8\n")
		return exitUsage
	}
	saveDir, err := a.cfg.EnsureDownloadDir()
	if err != nil {
		a.pr.errf("tuitai hls: %v\n", err)
		return exitIO
	}
	if *dir != "" {
		saveDir = *dir
	}
	name2 := *name
	if name2 == "" {
		name2 = strings.TrimSuffix(filepath.Base(strings.Split(fs.Arg(0), "?")[0]), ".m3u8")
		if name2 == "" {
			name2 = "stream"
		}
		if a.cfg.FFmpeg != "" {
			name2 += ".mp4"
		} else {
			name2 += ".ts"
		}
	}
	dest := a.dedupeDest(filepath.Join(saveDir, name2), nil)
	th := *threads
	if th <= 0 {
		th = 4
	}
	var lim *limiter.Limiter
	if a.cfg.RateLimit > 0 {
		lim = limiter.New(a.cfg.RateLimit, 0)
	}
	ctx, cancel := signalContext()
	defer cancel()
	res, err := hls.Run(ctx, mustClient(a.cfg), fs.Arg(0), dest, hls.Options{
		Threads: th, FFmpeg: a.cfg.FFmpeg, UserAgent: a.cfg.UserAgent,
		Referer: *dir, Limiter: lim,
	})
	if err != nil {
		a.pr.errf("tuitai hls: %v\n", err)
		return exitNet
	}
	if a.pr.asJSON {
		_ = a.pr.okJSON("hls", res)
		return exitOK
	}
	a.pr.humanf("Xong: %s (%d segment, %s, ghép bằng %s)\n",
		res.Dest, res.Segments, humanBytes(res.Bytes), res.Merged)
	if a.cfg.NotifyOnComplete {
		_ = notify.Notify(notify.Config{Enabled: true, Command: a.cfg.NotifyCommand},
			"Tui Download", "HLS xong: "+filepath.Base(res.Dest))
	}
	return exitOK
}

// cmdDoctor kiểm tra môi trường và in chẩn đoán.
func (a *app) cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "xuất JSON")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	a.pr.asJSON = *jsonOut
	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	var checks []check
	add := func(name string, ok bool, detail string) {
		checks = append(checks, check{name, ok, detail})
	}

	cfgPath, err := config.Path()
	add("thư mục cấu hình", err == nil, cfgPath)
	if dir, err := a.cfg.EnsureDownloadDir(); err == nil {
		free, ferr := diskfree.Free(dir)
		if ferr == nil {
			add("thư mục tải", true, fmt.Sprintf("%s (trống %s)", dir, humanBytes(int64(free))))
		} else {
			add("thư mục tải", true, dir)
		}
	} else {
		add("thư mục tải", false, err.Error())
	}
	if exe, err := exec.LookPath("ffmpeg"); err == nil {
		add("ffmpeg", true, exe)
	} else {
		add("ffmpeg", false, "không có (HLS vẫn ghép nhị phân được, không remux MP4)")
	}
	tool, _, okClip := clipboardTool()
	add("clipboard", okClip, tool)
	add("hệ điều hành", true, runtime.GOOS+"/"+runtime.GOARCH)
	add("phiên bản", true, version)
	if len(a.cfg.ScheduleWindow) > 0 {
		add("khung giờ tải", true, a.cfg.ScheduleWindow)
	} else {
		add("khung giờ tải", true, "mọi lúc")
	}

	if a.pr.asJSON {
		_ = a.pr.okJSON("doctor", checks)
		return exitOK
	}
	for _, c := range checks {
		mark := "OK  "
		if !c.OK {
			mark = "THIẾU"
		}
		a.pr.humanf("%-6s %-18s %s\n", mark, c.Name, c.Detail)
	}
	return exitOK
}

// writeCSV in danh sách job ra CSV.
func writeCSV(path string, recs []queueRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"id", "state", "bytes", "total", "url", "dest", "created_at"}); err != nil {
		return err
	}
	for _, r := range recs {
		if err := w.Write([]string{
			r.ID, r.State, strconv.FormatInt(r.Bytes, 10), strconv.FormatInt(r.Total, 10),
			r.URL, r.Dest, r.CreatedAt.Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

var _ = context.Background

// cmdExtension xuất extension Chrome MV3 ra đĩa (dùng cho "Load unpacked").
func (a *app) cmdExtension(args []string) int {
	fs := flag.NewFlagSet("extension", flag.ContinueOnError)
	out := fs.String("o", "", "thư mục xuất (mặc định: cạnh file cấu hình)")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	action := "export"
	if len(pos) > 0 {
		action = pos[0]
	}
	dir := *out
	if dir == "" {
		base, err := config.Path()
		if err != nil {
			a.pr.errf("tuitai extension: %v\n", err)
			return exitIO
		}
		dir = filepath.Join(filepath.Dir(base), "extension")
	}
	switch action {
	case "path":
		a.pr.humanf("%s\n", dir)
		return exitOK
	case "export", "install":
		n, err := extension.Export(dir)
		if err != nil {
			a.pr.errf("tuitai extension: %v\n", err)
			return exitIO
		}
		if a.pr.asJSON {
			_ = a.pr.okJSON("extension", map[string]any{
				"dir": dir, "files": n, "version": extension.Version(),
			})
			return exitOK
		}
		a.pr.humanf("Đã xuất %d file extension v%s vào:\n  %s\n", n, extension.Version(), dir)
		a.pr.humanf("Cài: mở chrome://extensions → bật Developer mode → Load unpacked → chọn thư mục trên.\n")
		if a.cfg.NotifyOnComplete {
			_ = notify.Notify(notify.Config{Enabled: true, Command: a.cfg.NotifyCommand},
				"Tui Download", "Extension đã xuất: "+dir)
		}
		return exitOK
	default:
		a.pr.errf("tuitai extension: hành động không hợp lệ %q (dùng: export | path)\n", action)
		return exitUsage
	}
}

// cmdAutostart bật/tắt chạy cùng hệ điều hành.
func (a *app) cmdAutostart(args []string) int {
	fs := flag.NewFlagSet("autostart", flag.ContinueOnError)
	quiet := fs.Bool("quiet", false, "im lặng")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	action := "status"
	if len(pos) > 0 {
		action = pos[0]
	}
	switch action {
	case "on", "enable":
		if err := autostart.Enable(); err != nil {
			a.pr.errf("tuitai autostart: %v\n", err)
			return exitIO
		}
	case "off", "disable":
		if err := autostart.Disable(); err != nil {
			a.pr.errf("tuitai autostart: %v\n", err)
			return exitIO
		}
	case "status":
	default:
		a.pr.errf("tuitai autostart: dùng on | off | status\n")
		return exitUsage
	}
	on := autostart.Enabled()
	if a.pr.asJSON {
		_ = a.pr.okJSON("autostart", map[string]any{"enabled": on, "location": autostart.Location()})
		return exitOK
	}
	state := "TẮT"
	if on {
		state = "BẬT"
	}
	a.pr.humanf("Tự chạy cùng hệ điều hành: %s\nNơi lưu: %s\n", state, autostart.Location())
	if on {
		a.pr.humanf("Lệnh khi khởi động: %s\n", mustCommand())
	}
	return exitOK
}

func mustCommand() string {
	c, err := autostart.Command()
	if err != nil {
		return "(không xác định)"
	}
	return c
}
