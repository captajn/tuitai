package diskfree

import (
	"errors"
	"fmt"
)

// ErrNotEnough báo không đủ chỗ trống.
var ErrNotEnough = errors.New("không đủ dung lượng trống")

// Check đảm bảo còn ít nhất need byte trống trên ổ chứa path (kèm 64 MiB dự phòng).
func Check(path string, need int64) error {
	if need <= 0 {
		return nil
	}
	free, err := Free(path)
	if err != nil {
		return nil // không kiểm tra được thì bỏ qua, không chặn tải
	}
	const reserve = 64 << 20
	if free < uint64(need)+reserve {
		return fmt.Errorf("%w: cần %d byte (kèm dự phòng), còn %d byte", ErrNotEnough, need+reserve, free)
	}
	return nil
}
