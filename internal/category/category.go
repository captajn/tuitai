// Package category phân loại file theo phần mở rộng/MIME và chọn thư mục con —
// tương đương các thư mục Video / Music / Documents / Compressed / Programs mà
// IDM tự tạo trong thư mục tải.
package category

import (
	"mime"
	"path/filepath"
	"strings"
)

// Kind là loại nội dung.
type Kind string

const (
	Video      Kind = "video"
	Music      Kind = "music"
	Documents  Kind = "documents"
	Compressed Kind = "compressed"
	Programs   Kind = "programs"
	Images     Kind = "images"
	Other      Kind = "other"
)

// defaultExts là bảng ánh xạ phần mở rộng mặc định.
var defaultExts = map[Kind][]string{
	Video: {"mp4", "mkv", "avi", "mov", "wmv", "flv", "webm", "m4v", "mpg", "mpeg", "ts", "m3u8", "3gp", "ogv"},
	Music: {"mp3", "flac", "wav", "aac", "m4a", "ogg", "opus", "wma", "aiff", "alac"},
	Documents: {"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "rtf", "epub",
		"mobi", "csv", "odt", "ods", "odp", "md", "json", "xml", "html", "htm"},
	Compressed: {"zip", "rar", "7z", "tar", "gz", "bz2", "xz", "iso", "cab", "zst", "tgz"},
	Programs: {"exe", "msi", "apk", "dmg", "pkg", "deb", "rpm", "appimage", "msix", "appx",
		"jar", "bin", "img", "bat", "sh", "ps1"},
	Images: {"jpg", "jpeg", "png", "gif", "bmp", "webp", "svg", "tif", "tiff", "ico", "heic", "avif"},
}

// defaultDirs là tên thư mục con mặc định cho từng loại.
var defaultDirs = map[Kind]string{
	Video:      "Video",
	Music:      "Music",
	Documents:  "Documents",
	Compressed: "Compressed",
	Programs:   "Programs",
	Images:     "Images",
}

// Classifier quyết định thư mục đích cho một file.
type Classifier struct {
	enabled bool
	base    string
	dirs    map[Kind]string
	ext     map[string]Kind
}

// New tạo Classifier. enabled=false nghĩa là luôn trả về base. overrides cho phép
// đổi tên thư mục con, ví dụ {"video": "Phim"}.
func New(base string, enabled bool, overrides map[string]string) *Classifier {
	dirs := make(map[Kind]string, len(defaultDirs))
	for k, v := range defaultDirs {
		dirs[k] = v
	}
	for k, v := range overrides {
		if v == "" {
			continue
		}
		dirs[Kind(strings.ToLower(k))] = v
	}
	ext := make(map[string]Kind, 96)
	for kind, list := range defaultExts {
		for _, e := range list {
			ext[e] = kind
		}
	}
	return &Classifier{enabled: enabled, base: base, dirs: dirs, ext: ext}
}

// KindOf xác định loại từ tên file và (tuỳ chọn) MIME.
func (c *Classifier) KindOf(name, mimeType string) Kind {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext != "" {
		if k, ok := c.ext[ext]; ok {
			return k
		}
	}
	if mimeType != "" {
		mt := mimeType
		if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
			mt = parsed
		}
		switch {
		case strings.HasPrefix(mt, "video/"):
			return Video
		case strings.HasPrefix(mt, "audio/"):
			return Music
		case strings.HasPrefix(mt, "image/"):
			return Images
		case strings.HasPrefix(mt, "text/"), mt == "application/pdf",
			mt == "application/msword", mt == "application/json":
			return Documents
		case strings.Contains(mt, "zip"), strings.Contains(mt, "compressed"),
			strings.Contains(mt, "tar"), strings.Contains(mt, "rar"), strings.Contains(mt, "7z"):
			return Compressed
		case strings.Contains(mt, "x-msdownload"), strings.Contains(mt, "x-msdos-program"),
			strings.Contains(mt, "vnd.android.package"), strings.Contains(mt, "x-apple-diskimage"):
			return Programs
		}
	}
	return Other
}

// Dir trả về thư mục đích (chưa tạo trên đĩa). Khi tắt phân loại hoặc loại
// "other" thì trả về đúng base.
func (c *Classifier) Dir(name, mimeType string) string {
	if !c.enabled {
		return c.base
	}
	k := c.KindOf(name, mimeType)
	sub, ok := c.dirs[k]
	if !ok || sub == "" {
		return c.base
	}
	return filepath.Join(c.base, sub)
}
