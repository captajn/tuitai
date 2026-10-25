// Package hls tải playlist HLS (.m3u8): chọn variant tốt nhất, tải song song
// các segment theo đúng thứ tự, ghép lại thành một file.
//
// Ghép mặc định là nối nhị phân (đúng với MPEG-TS và fMP4 có EXT-X-MAP). Nếu cấu
// hình có đường dẫn ffmpeg thì dùng ffmpeg để remux sang .mp4 cho chắc chắn.
package hls

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/captajn/tuitai/internal/limiter"
)

// Options điều khiển việc tải.
type Options struct {
	Threads    int
	FFmpeg     string // rỗng = chỉ nối nhị phân
	UserAgent  string
	Referer    string
	Limiter    *limiter.Limiter
	TempDir    string // rỗng = thư mục tạm hệ thống
	MergeToMP4 bool   // dùng ffmpeg remux sang mp4 nếu có
}

// Result mô tả kết quả.
type Result struct {
	Dest     string
	Segments int
	Bytes    int64
	Variant  string
	Merged   string // "concat" hoặc "ffmpeg"
	InitSeg  bool
}

// ErrNoSegment báo playlist rỗng.
var ErrNoSegment = errors.New("hls: playlist không có segment")

type entry struct {
	url   string
	order int
}

// parse đọc playlist và trả về (init segment, danh sách segment, danh sách
// variant con). Hỗ trợ EXT-X-MAP (init) và EXT-X-STREAM-INF (variant).
func parse(ctx context.Context, client *http.Client, rawURL, ua, referer string) (init string, segs []string, variants []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, nil, fmt.Errorf("hls: playlist trả %s", resp.Status)
	}
	base := resp.Request.URL
	abs := func(ref string) string {
		if ref == "" {
			return ""
		}
		u, e := base.Parse(strings.TrimSpace(ref))
		if e != nil {
			return ""
		}
		return u.String()
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 8<<20))
	expectVariant := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			expectVariant = true
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MAP") {
			if u := attr(line, "URI"); u != "" {
				init = abs(u)
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA") {
			if u := attr(line, "URI"); u != "" {
				variants = append(variants, abs(u))
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if expectVariant {
			variants = append(variants, abs(line))
			expectVariant = false
			continue
		}
		if u := abs(line); u != "" {
			segs = append(segs, u)
		}
	}
	if err := sc.Err(); err != nil {
		return "", nil, nil, err
	}
	return init, segs, variants, nil
}

func attr(line, key string) string {
	k := key + "="
	i := strings.Index(line, k)
	if i < 0 {
		return ""
	}
	rest := line[i+len(k):]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		if j := strings.IndexByte(rest[1:], '"'); j >= 0 {
			return rest[1 : 1+j]
		}
		return ""
	}
	if j := strings.IndexByte(rest, ','); j >= 0 {
		return rest[:j]
	}
	return strings.TrimRight(rest, "/> ")
}

// Run tải toàn bộ playlist rồi ghép thành dest.
func Run(ctx context.Context, client *http.Client, masterURL, dest string, opts Options) (*Result, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if opts.Threads <= 0 {
		opts.Threads = 4
	}
	init, segs, variants, err := parse(ctx, client, masterURL, opts.UserAgent, opts.Referer)
	if err != nil {
		return nil, err
	}
	variant := masterURL
	// Playlist master: chọn variant đầu tiên (thường là chất lượng cao nhất đã
	// được server xếp trước).
	if len(segs) == 0 && len(variants) > 0 {
		variant = variants[0]
		init, segs, _, err = parse(ctx, client, variant, opts.UserAgent, opts.Referer)
		if err != nil {
			return nil, err
		}
	}
	if len(segs) == 0 {
		return nil, ErrNoSegment
	}

	tmp := opts.TempDir
	if tmp == "" {
		tmp, err = os.MkdirTemp("", "tuitai-hls-*")
		if err != nil {
			return nil, err
		}
	} else {
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return nil, err
		}
	}
	if opts.TempDir == "" {
		defer os.RemoveAll(tmp)
	}

	var (
		mu      sync.Mutex
		parts   []entry
		wg      sync.WaitGroup
		errOnce error
		sem     = make(chan struct{}, opts.Threads)
		total   int64
	)
	dl := func(idx int, u string) {
		defer wg.Done()
		defer func() { <-sem }()
		path := filepath.Join(tmp, fmt.Sprintf("%06d.part", idx))
		n, err := fetchTo(ctx, client, u, path, opts)
		if err != nil {
			mu.Lock()
			if errOnce == nil {
				errOnce = fmt.Errorf("hls: segment %d: %w", idx, err)
			}
			mu.Unlock()
			return
		}
		mu.Lock()
		total += n
		parts = append(parts, entry{url: path, order: idx})
		mu.Unlock()
	}
	start := 0
	if init != "" {
		start = -1 // init segment đứng trước tất cả
		wg.Add(1)
		sem <- struct{}{}
		go dl(start, init)
	}
	for i, u := range segs {
		wg.Add(1)
		sem <- struct{}{}
		go dl(i, u)
	}
	wg.Wait()
	if errOnce != nil {
		return nil, errOnce
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].order < parts[j].order })

	order := make([]string, 0, len(parts))
	for _, p := range parts {
		order = append(order, p.url)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	method := "concat"
	if opts.FFmpeg != "" {
		if err := runFFmpeg(ctx, opts.FFmpeg, tmp, order, dest); err == nil {
			method = "ffmpeg"
			st, _ := os.Stat(dest)
			var size int64
			if st != nil {
				size = st.Size()
			}
			return &Result{Dest: dest, Segments: len(segs), Bytes: size, Variant: variant, Merged: method, InitSeg: init != ""}, nil
		}
		// ffmpeg lỗi → quay về nối nhị phân.
	}
	if err := concat(order, dest); err != nil {
		return nil, err
	}
	return &Result{Dest: dest, Segments: len(segs), Bytes: total, Variant: variant, Merged: method, InitSeg: init != ""}, nil
}

func fetchTo(ctx context.Context, client *http.Client, u, path string, opts Options) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		got, rerr := resp.Body.Read(buf)
		if got > 0 {
			if opts.Limiter != nil && opts.Limiter.Limited() {
				if err := opts.Limiter.WaitN(ctx, got); err != nil {
					return n, err
				}
			}
			if _, werr := f.Write(buf[:got]); werr != nil {
				return n, werr
			}
			n += int64(got)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, rerr
		}
	}
	return n, nil
}

func concat(files []string, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, p := range files {
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			return err
		}
		in.Close()
	}
	return out.Sync()
}

func runFFmpeg(ctx context.Context, ffmpeg, tmp string, files []string, dest string) error {
	list := filepath.Join(tmp, "concat.txt")
	f, err := os.Create(list)
	if err != nil {
		return err
	}
	for _, p := range files {
		fmt.Fprintf(f, "file '%s'\n", strings.ReplaceAll(p, "'", `'\''`))
	}
	f.Close()
	tmpOut := dest + ".tmp.mp4"
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", list, "-c", "copy", "-y", tmpOut)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmpOut, dest)
}

// ParseThreadsFromPlaylist không dùng; giữ chỗ cho định dạng mở rộng.

// Atoi an toàn cho cấu hình dạng chuỗi.
func Atoi(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
