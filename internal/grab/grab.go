// Package grab là phần "bắt link" kiểu IDM: tải một trang HTML rồi rút ra các
// link file/media, kể cả playlist HLS (.m3u8) và các segment bên trong.
//
// Đây là bộ trích xuất theo biểu thức chính quy, cố ý không kéo thêm thư viện
// HTML: đủ dùng cho trang tĩnh và cho các thẻ <a>/<video>/<source>/<img>.
package grab

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Link là một link tìm được.
type Link struct {
	URL  string
	Ext  string
	Kind string // "page", "file", "media", "playlist"
	Text string // chữ trong thẻ <a> nếu có
}

// Options điều khiển việc lọc.
type Options struct {
	// Exts chỉ giữ các phần mở rộng này (rỗng = giữ tất cả file có phần mở rộng).
	Exts []string
	// IncludePages giữ cả link không có phần mở rộng (trang khác).
	IncludePages bool
	// MaxLink giới hạn số link trả về (0 = 2000).
	MaxLink int
	// UserAgent, Referer gửi khi tải trang.
	UserAgent string
	Referer   string
}

// Lỗi công khai.
var (
	ErrNoLink  = errors.New("grab: không tìm thấy link phù hợp")
	ErrBadHTML = errors.New("grab: nội dung không phải HTML")
)

var (
	// attrRe bắt các thuộc tính chứa URL.
	attrRe = regexp.MustCompile(`(?is)(?:href|src|data-src|data-href|poster|content)\s*=\s*["']([^"']+)["']`)
	// anchorRe bắt <a ...>chữ</a> để lấy nhãn.
	anchorRe = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	// bareRe bắt URL trần trong script/nội dung.
	bareRe = regexp.MustCompile(`https?://[^\s"'<>\)]+`)
	// tagRe dùng để bỏ thẻ khỏi nhãn.
	tagRe = regexp.MustCompile(`(?s)<[^>]*>`)
)

var mediaExts = map[string]bool{
	"mp4": true, "mkv": true, "webm": true, "mov": true, "avi": true, "flv": true,
	"m4v": true, "mp3": true, "m4a": true, "aac": true, "flac": true, "ogg": true, "opus": true,
	"m3u8": true, "mpd": true, "ts": true,
}

// Extract rút link từ HTML. baseURL dùng để giải quyết đường dẫn tương đối.
func Extract(htmlText, baseURL string, opts Options) ([]Link, error) {
	if strings.TrimSpace(htmlText) == "" {
		return nil, ErrBadHTML
	}
	var base *url.URL
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("grab: base URL không hợp lệ: %w", err)
		}
		base = u
	}
	labels := map[string]string{}
	for _, m := range anchorRe.FindAllStringSubmatch(htmlText, -1) {
		key := resolve(base, m[1])
		if key == "" {
			continue
		}
		text := strings.TrimSpace(tagRe.ReplaceAllString(m[2], " "))
		text = strings.Join(strings.Fields(text), " ")
		if text != "" && labels[key] == "" {
			labels[key] = text
		}
	}

	keep := map[string]bool{}
	for _, e := range opts.Exts {
		keep[strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}
	max := opts.MaxLink
	if max <= 0 {
		max = 2000
	}

	seen := map[string]bool{}
	var out []Link
	add := func(raw string) {
		if len(out) >= max {
			return
		}
		abs := resolve(base, raw)
		if abs == "" || seen[abs] {
			return
		}
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(parsePath(abs)), "."))
		kind := "file"
		switch {
		case ext == "m3u8" || ext == "mpd":
			kind = "playlist"
		case mediaExts[ext]:
			kind = "media"
		case ext == "":
			if !opts.IncludePages {
				return
			}
			kind = "page"
		}
		if len(keep) > 0 && ext != "" && !keep[ext] {
			return
		}
		if len(keep) > 0 && ext == "" && !opts.IncludePages {
			return
		}
		seen[abs] = true
		out = append(out, Link{URL: abs, Ext: ext, Kind: kind, Text: labels[abs]})
	}

	for _, m := range attrRe.FindAllStringSubmatch(htmlText, -1) {
		add(m[1])
	}
	for _, m := range bareRe.FindAllString(htmlText, -1) {
		add(m)
	}
	if len(out) == 0 {
		return nil, ErrNoLink
	}
	return out, nil
}

// Page tải URL rồi trích link.
func Page(ctx context.Context, client *http.Client, pageURL string, opts Options) ([]Link, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("grab: trang trả %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(strings.ToLower(ct), "html") && !strings.Contains(strings.ToLower(ct), "text") {
		return nil, fmt.Errorf("%w: Content-Type %q", ErrBadHTML, ct)
	}
	return Extract(string(body), resp.Request.URL.String(), opts)
}

// Playlist đọc playlist HLS (.m3u8) và trả về các URL bên trong: variant
// (playlist con) hoặc segment. Link cuối cùng là thứ tự phát.
func Playlist(ctx context.Context, client *http.Client, m3u8URL string, opts Options) ([]Link, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m3u8URL, nil)
	if err != nil {
		return nil, err
	}
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("grab: playlist trả %s", resp.Status)
	}
	base, err := url.Parse(resp.Request.URL.String())
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	var out []Link
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
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") && strings.Contains(line, "URI=") {
			if u := attrURI(line); u != "" {
				if abs := resolve(base, u); abs != "" {
					out = append(out, Link{URL: abs, Kind: "playlist"})
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		abs := resolve(base, line)
		if abs == "" {
			continue
		}
		kind := "segment"
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(parsePath(abs)), "."))
		if expectVariant {
			kind = "playlist"
			ext = "m3u8"
			expectVariant = false
		}
		out = append(out, Link{URL: abs, Ext: ext, Kind: kind})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoLink
	}
	return out, nil
}

func attrURI(line string) string {
	const k = "URI="
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
	return rest
}

func resolve(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "javascript:") ||
		strings.HasPrefix(raw, "mailto:") || strings.HasPrefix(raw, "data:") {
		return ""
	}
	// Bỏ phần fragment để tránh trùng lặp.
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// parsePath lấy phần đường dẫn của URL đã tuyệt đối.
func parsePath(abs string) string {
	if u, err := url.Parse(abs); err == nil {
		return u.Path
	}
	return abs
}
