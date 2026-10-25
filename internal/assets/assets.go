// Package assets nhúng ảnh/logo vào binary để bản phát hành không cần file ngoài.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed *.png *.svg *.ico
var files embed.FS

// FS trả về hệ file nhúng (dùng cho HTTP /art/).
func FS() fs.FS { return files }

// Read đọc một file nhúng theo tên, ví dụ "empty-state-dark.png".
func Read(name string) ([]byte, error) { return files.ReadFile(name) }
