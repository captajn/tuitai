// Package extension nhúng extension Chrome MV3 và xuất ra đĩa để người dùng
// "Load unpacked" hoặc để bộ cài gọi sẵn.
package extension

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:files
var files embed.FS

// Export ghi toàn bộ file extension vào dir và trả về số file đã ghi.
func Export(dir string) (int, error) {
	n := 0
	err := fs.WalkDir(files, "files", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("files", p)
		if err != nil {
			return err
		}
		data, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

// Version đọc "version" trong manifest (dạng chuỗi đơn giản, không cần JSON parser).
func Version() string {
	data, err := files.ReadFile("files/manifest.json")
	if err != nil {
		return ""
	}
	s := string(data)
	key := "\"version\":"
	for i := 0; i+len(key) < len(s); i++ {
		if s[i:i+len(key)] == key {
			j := i + len(key)
			for j < len(s) && (s[j] == ' ' || s[j] == '"') {
				j++
			}
			k := j
			for k < len(s) && s[k] != '"' {
				k++
			}
			return s[j:k]
		}
	}
	return ""
}
