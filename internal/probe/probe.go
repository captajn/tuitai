// Package probe thăm dò một URL trước khi tải: kích thước, khả năng tải theo
// Range (tải song song), ETag/Last-Modified (để resume an toàn) và tên file gợi ý.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/captajn/tuitai/internal/strutil"
)

// ErrUnsupported: server trả về mã lỗi không thể tải.
type ErrUnsupported struct {
	StatusCode int
	Status     string
}

func (e ErrUnsupported) Error() string {
	return fmt.Sprintf("probe: server trả về %s", e.Status)
}

// Result là kết quả thăm dò.
type Result struct {
	URL          string // URL ban đầu
	FinalURL     string // URL sau redirect
	Size         int64  // -1 nếu server không công bố
	RangeOK      bool   // server chấp nhận Range
	ETag         string
	LastModified string
	Filename     string
	MIME         string
	StatusCode   int
}

// Options điều khiển cách thăm dò.
type Options struct {
	// Method là "HEAD" (mặc định) hoặc "GET".
	Method string
	// UserAgent, Referer áp cho request.
	UserAgent string
	Referer   string
	// Header là header bổ sung.
	Header http.Header
	// Username, Password cho Basic Auth.
	Username, Password string
	// FallbackToGet cho phép thử lại bằng GET khi HEAD bị từ chối (405/501).
	FallbackToGet bool
}

// Prober thực hiện thăm dò bằng một http.Client cho trước.
type Prober struct {
	client *http.Client
	opts   Options
}

// New tạo Prober; client nil sẽ dùng http.DefaultClient.
func New(client *http.Client, opts Options) *Prober {
	if client == nil {
		client = http.DefaultClient
	}
	if opts.Method == "" {
		opts.Method = http.MethodHead
	}
	return &Prober{client: client, opts: opts}
}

// Probe thăm dò url và trả về metadata.
func (p *Prober) Probe(ctx context.Context, rawURL string) (*Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("probe: URL không hợp lệ: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("probe: chỉ hỗ trợ http/https, nhận %q", u.Scheme)
	}

	res, err := p.once(ctx, rawURL, p.opts.Method)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusMethodNotAllowed || res.StatusCode == http.StatusNotImplemented {
		if p.opts.FallbackToGet {
			res, err = p.once(ctx, rawURL, http.MethodGet)
			if err != nil {
				return nil, err
			}
		}
	}
	if res.StatusCode >= 400 {
		out := res.Result
		return &out, ErrUnsupported{StatusCode: res.StatusCode, Status: res.status}
	}
	if res.Size < 0 && p.opts.Method == http.MethodHead {
		// Một số server không trả Content-Length cho HEAD; thử GET một byte.
		if g, gerr := p.getOneByte(ctx, rawURL); gerr == nil {
			if g.Size > 0 {
				res.Size = g.Size
			}
			if g.RangeOK {
				res.RangeOK = true
			}
		}
	}
	if res.Filename == "" {
		res.Filename = filenameFromURL(res.FinalURL)
	}
	res.Filename = strutil.Sanitize(res.Filename)
	out := res.Result
	return &out, nil
}

// innerResult bổ sung trường nội bộ.
type innerResult struct {
	Result
	status string
}

func (p *Prober) once(ctx context.Context, rawURL, method string) (*innerResult, error) {
	req, err := p.newRequest(ctx, method, rawURL)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	defer func() {
		// Đóng body mà không đọc hết: HEAD/GET thăm dò không cần nội dung.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 0))
		_ = resp.Body.Close()
	}()

	out := &innerResult{
		Result: Result{
			URL:          rawURL,
			FinalURL:     resp.Request.URL.String(),
			Size:         -1,
			RangeOK:      false,
			ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
			LastModified: resp.Header.Get("Last-Modified"),
			MIME:         mimeType(resp.Header.Get("Content-Type")),
			StatusCode:   resp.StatusCode,
		},
		status: resp.Status,
	}
	if resp.ContentLength > 0 {
		out.Size = resp.ContentLength
	} else if resp.ContentLength == 0 && method == http.MethodGet {
		out.Size = 0
	}
	out.RangeOK = strings.EqualFold(strings.TrimSpace(resp.Header.Get("Accept-Ranges")), "bytes")
	out.Filename = filenameFromDisposition(resp.Header.Get("Content-Disposition"))
	if out.Filename == "" {
		out.Filename = filenameFromURL(out.FinalURL)
	}
	return out, nil
}

// getOneByte gửi GET với Range: bytes=0-0 để lấy tổng kích thước từ Content-Range.
func (p *Prober) getOneByte(ctx context.Context, rawURL string) (*Result, error) {
	req, err := p.newRequest(ctx, http.MethodGet, rawURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	if resp.StatusCode != http.StatusPartialContent {
		return &Result{Size: resp.ContentLength, RangeOK: false}, nil
	}
	size := int64(-1)
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				size = n
			}
		}
	}
	return &Result{Size: size, RangeOK: true}, nil
}

func (p *Prober) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if p.opts.UserAgent != "" {
		req.Header.Set("User-Agent", p.opts.UserAgent)
	}
	if p.opts.Referer != "" {
		req.Header.Set("Referer", p.opts.Referer)
	}
	req.Header.Set("Accept", "*/*")
	for k, vs := range p.opts.Header {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	if p.opts.Username != "" || p.opts.Password != "" {
		req.SetBasicAuth(p.opts.Username, p.opts.Password)
	}
	return req, nil
}

func mimeType(contentType string) string {
	if contentType == "" {
		return ""
	}
	if mt, _, err := mime.ParseMediaType(contentType); err == nil {
		return mt
	}
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		return strings.TrimSpace(contentType[:i])
	}
	return strings.TrimSpace(contentType)
}

func filenameFromDisposition(cd string) string {
	if cd == "" {
		return ""
	}
	if _, params, err := mime.ParseMediaType(cd); err == nil {
		if fn := params["filename"]; fn != "" {
			// Một số server gửi filename* đã được chuẩn hoá về filename.
			return path.Base(strings.ReplaceAll(fn, "\\", "/"))
		}
	}
	return ""
}

func filenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if !strings.Contains(base, ".") {
		// Không có phần mở rộng: vẫn giữ nếu là tên hợp lý, ngược lại trả rỗng.
		if len(base) < 3 {
			return ""
		}
	}
	return base
}

// ErrNoSize báo server không công bố kích thước (không chia segment được).
var ErrNoSize = errors.New("probe: server không công bố kích thước file")
