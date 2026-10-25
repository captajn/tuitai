// Package config quản lý cấu hình ứng dụng: giá trị mặc định hợp lý, file
// JSON trong thư mục cấu hình của người dùng và các dẫn xuất (transport, proxy).
package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/captajn/tuitai/internal/strutil"
)

// Version là phiên bản định dạng file cấu hình.
const Version = 1

// Giới hạn và mặc định.
const (
	DefaultThreads       = 8
	MaxThreads           = 32
	DefaultMaxConcurrent = 3
	MaxMaxConcurrent     = 32
	DefaultRetries       = 5
	DefaultTimeout       = 30 * time.Second
	DefaultChunkBytes    = 1 << 20
	dirName              = "tuitai"
)

// Config là toàn bộ tuỳ chọn của ứng dụng.
type Config struct {
	Version int `json:"version"`

	// DownloadDir là thư mục đích mặc định cho job mới.
	DownloadDir string `json:"download_dir"`
	// Threads là số segment song song cho mỗi job (1..MaxThreads).
	Threads int `json:"threads"`
	// MaxConcurrent là số job được chạy đồng thời.
	MaxConcurrent int `json:"max_concurrent"`
	// RateLimit là giới hạn byte/giây toàn cục, 0 nghĩa là không giới hạn.
	RateLimit int64 `json:"rate_limit"`
	// PerJobRateLimit là giới hạn byte/giây cho từng job, 0 là không giới hạn.
	PerJobRateLimit int64 `json:"per_job_rate_limit"`
	// Proxy là URL proxy ("http://host:port", "socks5://…"); rỗng là trực tiếp.
	Proxy string `json:"proxy"`
	// UserAgent là header mặc định cho mọi request.
	UserAgent string `json:"user_agent"`
	// Referer là header Referer mặc định (rỗng thì không gửi).
	Referer string `json:"referer"`
	// Retries là số lần thử lại cho một segment trước khi bỏ cuộc.
	Retries int `json:"retries"`
	// ChunkBytes là kích thước một lần đọc/ghi, cũng là hạt chia nhỏ công việc.
	ChunkBytes int64 `json:"chunk_bytes"`
	// Resume cho phép tiếp tục job dở dang khi khởi động.
	Resume bool `json:"resume"`
	// VerifyTLS bật kiểm tra chứng chỉ HTTPS.
	VerifyTLS bool `json:"verify_tls"`
	// Timeout là hạn cho một request đơn lẻ (0 = mặc định của Go).
	Timeout time.Duration `json:"timeout"`
	// Mirrors là mirror dùng chung, áp dụng cho mọi job mới.
	Mirrors []string `json:"mirrors"`

	// OrganizeByType tự đưa file vào thư mục con theo loại (Video, Music, …).
	OrganizeByType bool `json:"organize_by_type"`
	// CategoryDirs ghi đè tên thư mục con cho từng loại (khoá: video, music, …).
	CategoryDirs map[string]string `json:"category_dirs,omitempty"`
	// ScheduleWindow giới hạn khung giờ được tải, dạng "HH:MM-HH:MM"; rỗng là mọi lúc.
	ScheduleWindow string `json:"schedule_window,omitempty"`
	// OffPeakRate là giới hạn tốc độ áp dụng ngoài ScheduleWindow (0 = dùng RateLimit).
	OffPeakRate int64 `json:"offpeak_rate,omitempty"`
	// NotifyOnComplete thông báo khi một job xong.
	NotifyOnComplete bool `json:"notify_on_complete"`
	// NotifyCommand là lệnh thông báo tuỳ chỉnh ({title}, {msg}); rỗng = mặc định hệ thống.
	NotifyCommand string `json:"notify_command,omitempty"`
	// FFmpeg là đường dẫn ffmpeg (rỗng = tự tìm trong PATH) dùng để ghép HLS sang MP4.
	FFmpeg string `json:"ffmpeg,omitempty"`

	// path ghi nhớ nơi cấu hình được nạp/ghi.
	path string
}

// Default trả về cấu hình mặc định theo nền tảng hiện tại.
func Default() *Config {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return &Config{
		Version:          Version,
		DownloadDir:      filepath.Join(home, "Downloads", dirName),
		Threads:          DefaultThreads,
		MaxConcurrent:    DefaultMaxConcurrent,
		Retries:          DefaultRetries,
		ChunkBytes:       DefaultChunkBytes,
		Resume:           true,
		VerifyTLS:        true,
		Timeout:          DefaultTimeout,
		UserAgent:        "tuitai/1.0 (+https://example.invalid/tuitai)",
		OrganizeByType:   true,
		NotifyOnComplete: true,
	}
}

// dir trả về thư mục cấu hình của ứng dụng theo os.UserConfigDir.
func dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: không xác định được thư mục cấu hình: %w", err)
	}
	return filepath.Join(base, dirName), nil
}

// Path trả về đường dẫn file cấu hình. Lỗi khi nền tảng không có thư mục cấu hình.
func Path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// PathOf trả về file cấu hình của một config đã nạp; rỗng nếu chưa gắn nguồn.
func (c *Config) PathOf() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Load đọc cấu hình từ thư mục người dùng. File không tồn tại trả về cấu hình
// mặc định (không lỗi) để lần chạy đầu tiên vẫn dùng được.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		c := Default()
		return c, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c := Default()
			c.path = p
			return c, nil
		}
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: JSON hỏng trong %s: %w", p, err)
	}
	c.path = p
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	return c, nil
}

// Save ghi cấu hình nguyên tử vào file của nó (hoặc file mặc định nếu chưa gắn).
func (c *Config) Save() error {
	if c == nil {
		return errors.New("config: nil")
	}
	p := c.path
	if p == "" {
		var err error
		if p, err = Path(); err != nil {
			return err
		}
		c.path = p
	}
	if err := c.Normalize(); err != nil {
		return err
	}
	c.Version = Version
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// SetPath gắn nguồn cấu hình (dùng cho test hoặc cờ --config).
func (c *Config) SetPath(p string) { c.path = p }

// Normalize đưa các trường về miền giá trị hợp lệ và bù mặc định.
func (c *Config) Normalize() error {
	if c.Threads <= 0 {
		c.Threads = DefaultThreads
	}
	if c.Threads > MaxThreads {
		c.Threads = MaxThreads
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = DefaultMaxConcurrent
	}
	if c.MaxConcurrent > MaxMaxConcurrent {
		c.MaxConcurrent = MaxMaxConcurrent
	}
	if c.Retries < 0 {
		c.Retries = 0
	}
	if c.ChunkBytes <= 0 {
		c.ChunkBytes = DefaultChunkBytes
	}
	if c.ChunkBytes > 64<<20 {
		c.ChunkBytes = 64 << 20
	}
	if c.RateLimit < 0 {
		c.RateLimit = 0
	}
	if c.PerJobRateLimit < 0 {
		c.PerJobRateLimit = 0
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.UserAgent == "" {
		c.UserAgent = Default().UserAgent
	}
	if c.DownloadDir == "" {
		c.DownloadDir = Default().DownloadDir
	}
	// Chặn đường dẫn tương đối khó đoán: đổi sang tuyệt đối.
	if abs, err := filepath.Abs(c.DownloadDir); err == nil {
		c.DownloadDir = abs
	}
	if err := c.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate kiểm tra các ràng buộc không thể tự sửa.
func (c *Config) Validate() error {
	if c.Proxy != "" {
		u, err := url.Parse(c.Proxy)
		if err != nil {
			return fmt.Errorf("config: proxy không hợp lệ: %w", err)
		}
		switch u.Scheme {
		case "http", "https", "socks5":
			if u.Host == "" {
				return fmt.Errorf("config: proxy thiếu host: %q", c.Proxy)
			}
		default:
			return fmt.Errorf("config: scheme proxy không hỗ trợ: %q", u.Scheme)
		}
	}
	for _, m := range c.Mirrors {
		if m == "" {
			return errors.New("config: mirror rỗng")
		}
	}
	return nil
}

// EnsureDownloadDir tạo thư mục tải nếu chưa có và trả về đường dẫn tuyệt đối.
func (c *Config) EnsureDownloadDir() (string, error) {
	if c == nil {
		return "", errors.New("config: nil")
	}
	d := c.DownloadDir
	if d == "" {
		d = Default().DownloadDir
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// TempName gợi ý tên file an toàn cho URL (chưa ghép thư mục).
func TempName(rawURL string) string { return strutil.FilenameFromURL(rawURL) }

// NewTransport dựng http.Transport theo cấu hình (proxy, TLS, số kết nối).
// Kèm http2 mặc định của Go cho HTTPS.
func (c *Config) NewTransport() (*http.Transport, error) {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   c.Threads + 4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    false,
	}
	if !c.VerifyTLS {
		// #nosec G402 -- người dùng chủ động tắt kiểm tra chứng chỉ.
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if c.Proxy != "" {
		u, err := url.Parse(c.Proxy)
		if err != nil {
			return nil, err
		}
		t.Proxy = http.ProxyURL(u)
	}
	return t, nil
}

// NewClient dựng http.Client dùng transport theo cấu hình.
func (c *Config) NewClient() (*http.Client, error) {
	tr, err := c.NewTransport()
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
		Timeout:   c.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("config: quá nhiều redirect")
			}
			return nil
		},
	}, nil
}

// Summary mô tả cấu hình một dòng cho UI.
func (c *Config) Summary() string {
	if c == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("dir=%s", c.DownloadDir),
		fmt.Sprintf("threads=%d", c.Threads),
		fmt.Sprintf("concurrent=%d", c.MaxConcurrent),
		fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	if c.RateLimit > 0 {
		parts = append(parts, "rate="+strutil.Speed(float64(c.RateLimit)))
	}
	if c.Proxy != "" {
		parts = append(parts, "proxy="+c.Proxy)
	}
	return strings.Join(parts, " ")
}
