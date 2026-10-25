// Package verify kiểm tra tính toàn vẹn của file đã tải trước khi công bố.
//
// Gồm ba việc tách biệt: so khớp độ dài với kích thước server công bố, so khớp
// checksum tuỳ chọn, và đổi tên file tạm ".part" thành tên thật.
package verify

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Algorithm là thuật toán băm được hỗ trợ.
type Algorithm string

const (
	// SHA256 là thuật toán mặc định khi người dùng không nêu rõ.
	SHA256 Algorithm = "sha256"
	// MD5 chỉ dùng khi nguồn cung cấp sẵn giá trị MD5.
	MD5 Algorithm = "md5"
)

// PartSuffix là hậu tố của file đang tải dở.
const PartSuffix = ".part"

var (
	// ErrLengthMismatch: file không đúng độ dài mong đợi.
	ErrLengthMismatch = errors.New("verify: độ dài file không khớp")
	// ErrChecksumMismatch: băm không khớp giá trị mong đợi.
	ErrChecksumMismatch = errors.New("verify: checksum không khớp")
	// ErrEmptyChecksum: spec checksum thiếu giá trị hoặc thuật toán.
	ErrEmptyChecksum = errors.New("verify: checksum rỗng hoặc thuật toán không hỗ trợ")
)

// Spec là một yêu cầu kiểm tra checksum.
type Spec struct {
	Algorithm Algorithm `json:"algorithm"`
	Value     string    `json:"value"` // hex, không phân biệt hoa thường
}

// ParseSpec đọc chuỗi dạng "algo:hex" (ví dụ "sha256:ab12…"); mặc định SHA-256
// khi không có tiền tố. Giá trị phải là hex đúng độ dài của thuật toán.
func ParseSpec(s string) (*Spec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrEmptyChecksum
	}
	algo := SHA256
	if i := strings.IndexByte(s, ':'); i >= 0 {
		algo = Algorithm(strings.ToLower(s[:i]))
		s = strings.TrimSpace(s[i+1:])
	}
	if _, err := hex.DecodeString(s); err != nil {
		return nil, fmt.Errorf("verify: checksum không phải hex: %w", err)
	}
	want := map[Algorithm]int{SHA256: 64, MD5: 32}[algo]
	if want == 0 {
		return nil, fmt.Errorf("verify: thuật toán không hỗ trợ %q", algo)
	}
	if len(s) != want {
		return nil, fmt.Errorf("verify: checksum %s phải dài %d hex", algo, want)
	}
	return &Spec{Algorithm: algo, Value: strings.ToLower(s)}, nil
}

// String biểu diễn spec ở dạng "algo:hex".
func (s *Spec) String() string {
	if s == nil {
		return ""
	}
	return string(s.Algorithm) + ":" + s.Value
}

func newHash(a Algorithm) (hash.Hash, bool) {
	switch a {
	case SHA256, "":
		return sha256.New(), true
	case MD5:
		return md5.New(), true
	default:
		return nil, false
	}
}

// Sum băm toàn bộ file theo thuật toán của spec.
func (s *Spec) Sum(path string) (string, error) {
	if s == nil {
		return "", ErrEmptyChecksum
	}
	h, ok := newHash(s.Algorithm)
	if !ok {
		return "", fmt.Errorf("verify: thuật toán không hỗ trợ %q", s.Algorithm)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Validate băm file và so sánh với giá trị mong đợi.
func (s *Spec) Validate(path string) error {
	if s == nil || s.Value == "" {
		return ErrEmptyChecksum
	}
	got, err := s.Sum(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, s.Value) {
		return fmt.Errorf("%w: mong đợi %s, nhận %s", ErrChecksumMismatch, s.Value, got)
	}
	return nil
}

// CheckLength đối chiếu kích thước file trên đĩa với kích thước mong đợi.
// want < 0 nghĩa là server không công bố kích thước nên bỏ qua kiểm tra.
func CheckLength(path string, want int64) error {
	if want < 0 {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Size() != want {
		return fmt.Errorf("%w: mong đợi %d, nhận %d", ErrLengthMismatch, want, st.Size())
	}
	return nil
}

// Finalize công bố file tạm bằng cách đổi tên thành tên thật. Nếu file đích đã
// tồn tại, nó bị thay thế (os.Rename ghi đè được trên cả Windows).
func Finalize(partPath, finalPath string) error {
	if partPath == "" || finalPath == "" {
		return errors.New("verify: đường dẫn rỗng")
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	return os.Rename(partPath, finalPath)
}

// Verify chạy toàn bộ kiểm tra bắt buộc: độ dài, rồi checksum nếu có.
func Verify(path string, wantSize int64, spec *Spec) error {
	if err := CheckLength(path, wantSize); err != nil {
		return err
	}
	if spec != nil {
		return spec.Validate(path)
	}
	return nil
}

// PartPath trả về đường dẫn file tạm cạnh file đích.
func PartPath(dest string) string { return dest + PartSuffix }
