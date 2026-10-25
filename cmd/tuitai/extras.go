package main

// extras.go: các lệnh "kiểu IDM" bổ sung — bắt link từ trang (grab), nhập danh
// sách URL (import), lọc/sắp xếp danh sách job.

import (
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/grab"
	"github.com/captajn/tuitai/internal/queue"
	"github.com/captajn/tuitai/internal/strutil"
)

// enqueueURL thêm một URL vào hàng đợi và ghi bền — dùng chung cho daemon,
// clipboard và TUI.
func (a *app) enqueueURL(rawURL, dir string, threads int) (queueRecord, error) {
	dest, err := a.resolveDest(rawURL, dir, "")
	if err != nil {
		return queueRecord{}, err
	}
	dest = a.dedupeDest(dest, nil)
	if threads <= 0 {
		threads = a.cfg.Threads
	}
	rec := queueRecord{
		ID: newID(), URL: rawURL, Dest: dest, Threads: threads,
		State: string(queue.StateQueued), CreatedAt: time.Now(),
	}
	if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
		return queueRecord{}, err
	}
	recs, _ := a.loadRecords()
	recs = append(recs, rec)
	if err := a.saveRecords(recs); err != nil {
		return queueRecord{}, err
	}
	return rec, nil
}

// parseArgs tách cờ và tham số vị trí để cho phép viết cờ SAU URL
// (gói flag mặc định dừng phân tích ở tham số không phải cờ đầu tiên).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' && a != "--" {
			flags = append(flags, a)
			// Cờ dạng -k v cần kéo theo giá trị của nó.
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if f := fs.Lookup(name); f != nil {
					isBool := false
					if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok {
						isBool = bf.IsBoolFlag()
					}
					if !isBool && i+1 < len(args) {
						flags = append(flags, args[i+1])
						i++
					}
				}
			}
			continue
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return pos, nil
}

// filterRecords lọc theo chuỗi con (không phân biệt hoa thường) trong ID, URL,
// đường dẫn đích và trạng thái.
func filterRecords(recs []queueRecord, needle string) []queueRecord {
	needle = strings.TrimSpace(strings.ToLower(needle))
	if needle == "" {
		return recs
	}
	out := make([]queueRecord, 0, len(recs))
	for _, r := range recs {
		hay := strings.ToLower(r.ID + " " + r.URL + " " + r.Dest + " " + r.State)
		if strings.Contains(hay, needle) {
			out = append(out, r)
		}
	}
	return out
}

// sortRecords sắp xếp tại chỗ. key rỗng thì giữ nguyên thứ tự.
func sortRecords(recs []queueRecord, key string) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "":
		return
	case "id":
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	case "state":
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].State < recs[j].State })
	case "size", "total":
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].Total > recs[j].Total })
	case "progress":
		pct := func(r queueRecord) float64 {
			if r.Total <= 0 {
				return -1
			}
			return float64(r.Bytes) / float64(r.Total)
		}
		sort.SliceStable(recs, func(i, j int) bool { return pct(recs[i]) > pct(recs[j]) })
	case "date":
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].CreatedAt.After(recs[j].CreatedAt) })
	}
}

// cmdGrab tìm link trên trang rồi (mặc định) đưa vào hàng đợi và tải luôn.
func (a *app) cmdGrab(args []string) int {
	fs := flag.NewFlagSet("grab", flag.ContinueOnError)
	exts := fs.String("ext", "", "chỉ lấy các phần mở rộng này (v.d. mp4,mkv,m3u8)")
	pages := fs.Bool("pages", false, "lấy cả link trang không có phần mở rộng")
	limit := fs.Int("limit", 0, "giới hạn số link (0 = 2000)")
	threads := fs.Int("t", 0, "số kết nối mỗi job")
	dir := fs.String("o", "", "thư mục lưu")
	noRun := fs.Bool("no-run", false, "chỉ xếp hàng")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	if len(pos) == 0 {
		a.pr.errf("tuitai grab: cần URL trang\n")
		return exitUsage
	}

	var extList []string
	if *exts != "" {
		for _, e := range strings.Split(*exts, ",") {
			if e = strings.TrimSpace(e); e != "" {
				extList = append(extList, e)
			}
		}
	}
	saveDir, err := a.cfg.EnsureDownloadDir()
	if err != nil {
		a.pr.errf("tuitai grab: %v\n", err)
		return exitIO
	}
	if *dir != "" {
		saveDir = *dir
	}
	client := mustClient(a.cfg)
	var all []grab.Link
	for _, pageURL := range pos {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		links, gerr := grab.Page(ctx, client, pageURL, grab.Options{
			Exts: extList, IncludePages: *pages, MaxLink: *limit,
			UserAgent: a.cfg.UserAgent, Referer: pageURL,
		})
		cancel()
		if gerr != nil {
			a.pr.errf("tuitai grab: %s: %v\n", pageURL, gerr)
			return exitNet
		}
		all = append(all, links...)
	}

	if !a.pr.asJSON && !a.pr.quiet {
		rows := make([][]string, 0, len(all))
		for _, l := range all {
			label := l.Text
			if label == "" {
				label = filepath.Base(l.URL)
			}
			rows = append(rows, []string{l.Kind, l.Ext, truncateURL(l.URL, 70), truncateURL(label, 30)})
		}
		a.pr.table([]string{"LOẠI", "EXT", "URL", "NHÃN"}, rows)
		a.pr.humanf("Tìm thấy %d link.\n", len(all))
	}
	if *noRun {
		if a.pr.asJSON {
			_ = a.pr.okJSON("grab", all)
		}
		return exitOK
	}

	threadsN := *threads
	if threadsN <= 0 {
		threadsN = a.cfg.Threads
	}
	a.setup(0, nil)
	var added []queueRecord
	pending := map[string]bool{}
	for _, l := range all {
		if l.Kind == "page" {
			continue // không tự tải trang điều hướng
		}
		dest, derr := a.resolveDest(l.URL, saveDir, "")
		if derr != nil {
			a.pr.errf("tuitai grab: %s: %v\n", l.URL, derr)
			continue
		}
		dest = a.dedupeDest(dest, pending)
		rec := queueRecord{
			ID: newID(), URL: l.URL, Dest: dest, Threads: threadsN,
			Referer: "", State: string(queue.StateQueued), CreatedAt: time.Now(),
		}
		added = append(added, rec)
		if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
			a.pr.errf("tuitai grab: %v\n", err)
		}
	}
	if len(added) == 0 {
		a.pr.errf("tuitai grab: không có link nào để tải\n")
		return exitNotFound
	}
	recs, _ := a.loadRecords()
	recs = append(recs, added...)
	_ = a.saveRecords(recs)
	if a.pr.asJSON {
		_ = a.pr.okJSON("grab", added)
	}
	return a.runQueue(context.Background(), nil)
}

// cmdImport đọc danh sách URL từ file (hoặc stdin với "-").
func (a *app) cmdImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	threads := fs.Int("t", 0, "số kết nối mỗi job")
	dir := fs.String("o", "", "thư mục lưu")
	priority := fs.Int("priority", 0, "mức ưu tiên")
	noRun := fs.Bool("no-run", false, "chỉ xếp hàng")
	jsonOut := fs.Bool("json", false, "xuất JSON")
	quiet := fs.Bool("quiet", false, "im lặng")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return exitUsage
	}
	a.pr.asJSON, a.pr.quiet = *jsonOut, *quiet
	if len(pos) == 0 {
		a.pr.errf("tuitai import: cần đường dẫn file (hoặc - để đọc stdin)\n")
		return exitUsage
	}

	var raw []byte
	src := pos[0]
	if src == "-" {
		raw, err = io.ReadAll(io.LimitReader(os.Stdin, 8<<20))
	} else {
		raw, err = os.ReadFile(src)
	}
	if err != nil {
		a.pr.errf("tuitai import: %v\n", err)
		return exitIO
	}

	urls := parseURLList(string(raw))
	if len(urls) == 0 {
		a.pr.errf("tuitai import: không có URL hợp lệ trong %s\n", src)
		return exitUsage
	}
	saveDir, err := a.cfg.EnsureDownloadDir()
	if err != nil {
		a.pr.errf("tuitai import: %v\n", err)
		return exitIO
	}
	if *dir != "" {
		saveDir = *dir
	}
	threadsN := *threads
	if threadsN <= 0 {
		threadsN = a.cfg.Threads
	}

	a.setup(0, nil)
	var added []queueRecord
	pending := map[string]bool{}
	for _, u := range urls {
		dest, derr := a.resolveDest(u, saveDir, "")
		if derr != nil {
			a.pr.errf("tuitai import: %s: %v\n", u, derr)
			continue
		}
		dest = a.dedupeDest(dest, pending)
		rec := queueRecord{
			ID: newID(), URL: u, Dest: dest, Threads: threadsN, Priority: *priority,
			State: string(queue.StateQueued), CreatedAt: time.Now(),
		}
		added = append(added, rec)
		if err := a.q.Add(a.jobFromRecord(rec)); err != nil {
			a.pr.errf("tuitai import: %v\n", err)
		}
	}
	if len(added) == 0 {
		return exitNotFound
	}
	recs, _ := a.loadRecords()
	recs = append(recs, added...)
	_ = a.saveRecords(recs)
	if a.pr.asJSON {
		_ = a.pr.okJSON("import", added)
	} else {
		a.pr.humanf("Đã thêm %d URL.\n", len(added))
	}
	if *noRun {
		return exitOK
	}
	return a.runQueue(context.Background(), nil)
}

// parseURLList tách URL từ văn bản: mỗi dòng một URL, bỏ dòng trống và chú thích
// bằng '#'. Chấp nhận cả dạng "URL<TAB>tên-file".
func parseURLList(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexAny(line, "\t "); i > 0 {
			line = line[:i]
		}
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") &&
			!strings.HasPrefix(line, "ftp://") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

var _ = strutil.Sanitize
