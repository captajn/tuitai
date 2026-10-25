// Package state lưu trạng thái tải dở để có thể RESUME sau khi tắt ứng dụng.
//
// Mỗi job có một file JSON cạnh file đích, tên `<file>.tuitai`. File được ghi
// nguyên tử (ghi file tạm rồi rename) nên không bao giờ đọc phải nội dung dở.
// Trước khi resume, ETag/Last-Modified được đối chiếu với server: nếu khác thì
// dữ liệu cũ bị coi là không dùng được và job phải tải lại từ đầu.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/captajn/tuitai/internal/verify"
)

// Suffix là hậu tố file trạng thái.
const Suffix = ".tuitai"

// Version là phiên bản định dạng hiện tại; file khác version bị từ chối.
const Version = 1

var (
	// ErrNoState: không có file trạng thái cho file đích.
	ErrNoState = errors.New("state: không có trạng thái đã lưu")
	// ErrVersion: file trạng thái thuộc phiên bản không hỗ trợ.
	ErrVersion = errors.New("state: phiên bản trạng thái không hỗ trợ")
	// ErrIncompatible: server đã đổi nội dung so với lúc lưu.
	ErrIncompatible = errors.New("state: nội dung trên server đã thay đổi")
)

// Segment là ảnh chụp một segment: Next là offset kế tiếp cần tải, nhờ đó lần
// chạy sau biết chính xác đã tải được tới đâu.
type Segment struct {
	ID    int   `json:"id"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`  // inclusive
	Next  int64 `json:"next"` // offset kế tiếp; > End nghĩa là đã xong
}

// Done cho biết segment đã tải xong.
func (s Segment) Done() bool { return s.Next > s.End }

// Job là toàn bộ trạng thái cần thiết để resume một lần tải.
type Job struct {
	Version      int          `json:"version"`
	ID           string       `json:"id"`
	URL          string       `json:"url"`
	FinalURL     string       `json:"final_url,omitempty"`
	Dest         string       `json:"dest"`
	Size         int64        `json:"size"`
	RangeOK      bool         `json:"range_ok"`
	ETag         string       `json:"etag,omitempty"`
	LastModified string       `json:"last_modified,omitempty"`
	MIME         string       `json:"mime,omitempty"`
	Filename     string       `json:"filename,omitempty"`
	Threads      int          `json:"threads"`
	Segments     []Segment    `json:"segments,omitempty"`
	Checksum     *verify.Spec `json:"checksum,omitempty"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Store là nơi đọc/ghi trạng thái. FileStore là hiện thực mặc định.
type Store interface {
	// Load đọc trạng thái của file đích.
	Load(dest string) (*Job, error)
	// Save ghi đè trạng thái của job một cách nguyên tử.
	Save(j *Job) error
	// Remove xoá trạng thái của file đích (không lỗi nếu không có).
	Remove(dest string) error
}

// PathFor trả về đường dẫn file trạng thái của file đích.
func PathFor(dest string) string { return dest + Suffix }

// FileStore lưu trạng thái vào filesystem.
type FileStore struct {
	// mu tuần tự hoá các lần ghi cùng file trong tiến trình.
	mu sync.Map // dest -> *sync.Mutex
}

func (s *FileStore) lockFor(dest string) *sync.Mutex {
	v, _ := s.mu.LoadOrStore(dest, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Load đọc và kiểm tra hợp lệ file trạng thái.
func (s *FileStore) Load(dest string) (*Job, error) {
	data, err := os.ReadFile(PathFor(dest))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoState
		}
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("state: JSON hỏng: %w", err)
	}
	if j.Version != Version {
		return nil, fmt.Errorf("%w: %d", ErrVersion, j.Version)
	}
	if j.Dest == "" {
		j.Dest = dest
	}
	return &j, nil
}

// Save ghi trạng thái: ghi file tạm cùng thư mục, fsync rồi rename đè.
func (s *FileStore) Save(j *Job) error {
	if j == nil || j.Dest == "" {
		return errors.New("state: job rỗng")
	}
	j.Version = Version
	j.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	path := PathFor(j.Dest)

	mu := s.lockFor(j.Dest)
	mu.Lock()
	defer mu.Unlock()

	// Tên tạm gắn PID trong cùng thư mục để rename là cùng ổ đĩa.
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Remove xoá file trạng thái, bỏ qua trường hợp không tồn tại.
func (s *FileStore) Remove(dest string) error {
	mu := s.lockFor(dest)
	mu.Lock()
	defer mu.Unlock()
	err := os.Remove(PathFor(dest))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// List quét dir và nạp mọi trạng thái hợp lệ, sắp xếp theo thời gian cập nhật
// giảm dần. Dùng khi khởi động để khôi phục hàng đợi sau khi tắt app.
func List(dir string) ([]*Job, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Suffix) {
			continue
		}
		dest := filepath.Join(dir, strings.TrimSuffix(e.Name(), Suffix))
		var j Job
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &j); err != nil || j.Version != Version {
			continue
		}
		if j.Dest == "" {
			j.Dest = dest
		}
		jobs = append(jobs, &j)
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].UpdatedAt.After(jobs[b].UpdatedAt) })
	return jobs, nil
}

// Compatible kiểm tra server còn phục vụ đúng nội dung đã lưu hay không.
// ETag được ưu tiên; nếu một trong hai bên không có ETag thì dựa vào
// Last-Modified. Kích thước lệch nhau luôn là không tương thích.
func Compatible(saved *Job, size int64, etag, lastModified string) bool {
	if saved == nil || saved.Size <= 0 {
		return false
	}
	if saved.Size != size {
		return false
	}
	if saved.ETag != "" && etag != "" {
		return saved.ETag == etag
	}
	if saved.LastModified != "" && lastModified != "" {
		return saved.LastModified == lastModified
	}
	// Không còn dấu vết nào để so: chỉ chấp nhận khi server cũng không cung cấp
	// ETag lẫn Last-Modified và độ dài trùng khớp (đã kiểm ở trên).
	return saved.ETag == "" && etag == "" && saved.LastModified == "" && lastModified == ""
}

// Normalize chuẩn hoá danh sách segment: bỏ segment đã xong, sắp theo Start.
func Normalize(segs []Segment) []Segment {
	out := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if s.Done() {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// DoneBytes cộng số byte đã tải xong theo trạng thái đã lưu.
func DoneBytes(segs []Segment) int64 {
	var n int64
	for _, s := range segs {
		n += s.Next - s.Start
		if s.Done() {
			n += s.End + 1 - s.Next
		}
	}
	return n
}
