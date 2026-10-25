package probe

import (
	"bytes"
	"errors"
	"io"
	"time"
)

// Các tiện ích chỉ dùng trong test.

func timeZero() time.Time { return time.Time{} }

func readerOf(b []byte) io.ReadSeeker { return bytes.NewReader(b) }

func asErr(err error, target any) bool { return errors.As(err, target) }
