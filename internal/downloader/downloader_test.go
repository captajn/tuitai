package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/captajn/tuitai/internal/state"
	"github.com/captajn/tuitai/internal/verify"
)

// payload tạo dữ liệu xác định để so sánh từng byte sau khi tải.
func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}

// rangeServer phục vụ dữ liệu có hỗ trợ Range (http.ServeContent).
func rangeServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag-1"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		http.ServeContent(w, r, "data.bin", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newDL(t *testing.T) *Downloader {
	t.Helper()
	return New(Options{StateStore: &state.FileStore{}})
}

func TestSegmentedDownload(t *testing.T) {
	data := payload(500_000)
	ts := rangeServer(t, data)
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")

	d := newDL(t)
	res, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/data.bin", Dest: dest, Threads: 4, Retries: 2, Resume: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("đọc file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("nội dung sai: %d byte nhận được", len(got))
	}
	if res.Bytes != int64(len(data)) {
		t.Fatalf("Bytes = %d, muốn %d", res.Bytes, len(data))
	}
	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file .part phải được đổi tên sau khi xong")
	}
	if _, err := os.Stat(state.PathFor(dest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("trạng thái phải bị xoá sau khi xong")
	}
}

func TestSingleStreamNoRange(t *testing.T) {
	data := payload(120_000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "120000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer ts.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "single.bin")
	d := newDL(t)
	if _, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/single.bin", Dest: dest, Threads: 8, Retries: 1,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("nội dung tải một luồng sai")
	}
}

func TestResumeContinuesFromState(t *testing.T) {
	data := payload(400_000)
	ts := rangeServer(t, data)
	dir := t.TempDir()
	dest := filepath.Join(dir, "resume.bin")
	part := verify.PartPath(dest)

	// Giả lập lần chạy trước: đã tải 100000 byte đầu.
	if err := os.WriteFile(part, data[:100_000], 0o644); err != nil {
		t.Fatal(err)
	}
	st := &state.FileStore{}
	saved := &state.Job{
		Version: state.Version, URL: ts.URL + "/resume.bin", Dest: dest,
		Size: int64(len(data)), RangeOK: true, ETag: "etag-1",
		LastModified: "Mon, 02 Jan 2006 15:04:05 GMT", Threads: 2,
		Segments: []state.Segment{
			{ID: 1, Start: 0, End: 199_999, Next: 100_000},
			{ID: 2, Start: 200_000, End: 399_999, Next: 200_000},
		},
	}
	if err := st.Save(saved); err != nil {
		t.Fatal(err)
	}

	d := New(Options{StateStore: st})
	res, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/resume.bin", Dest: dest, Threads: 2, Resume: true, Retries: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Resumed {
		t.Fatal("Resumed = false, muốn true")
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("nội dung sau resume sai")
	}
}

func TestResumeRejectsChangedRemote(t *testing.T) {
	data := payload(200_000)
	ts := rangeServer(t, data)
	dir := t.TempDir()
	dest := filepath.Join(dir, "changed.bin")
	part := verify.PartPath(dest)
	_ = os.WriteFile(part, data[:50_000], 0o644)

	st := &state.FileStore{}
	// ETag khác với server => phải tải lại từ đầu.
	_ = st.Save(&state.Job{
		Version: state.Version, URL: ts.URL + "/changed.bin", Dest: dest,
		Size: int64(len(data)), RangeOK: true, ETag: "etag-cu",
		Segments: []state.Segment{{ID: 1, Start: 0, End: int64(len(data)) - 1, Next: 50_000}},
	})

	d := New(Options{StateStore: st})
	res, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/changed.bin", Dest: dest, Threads: 2, Resume: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Resumed {
		t.Fatal("không được resume khi ETag đổi")
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("nội dung sau khi tải lại sai")
	}
}

// slowServer gửi dữ liệu chậm để có thể huỷ giữa chừng.
func slowServer(data []byte, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		if rg := r.Header.Get("Range"); rg != "" {
			w.Header().Set("Content-Range", "bytes 0-"+itoa(len(data)-1)+"/"+itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		chunk := len(data) / 20
		for off := 0; off < len(data); off += chunk {
			end := off + chunk
			if end > len(data) {
				end = len(data)
			}
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(delay)
		}
	}))
}

func TestCancelKeepsState(t *testing.T) {
	data := payload(200_000)
	ts := slowServer(data, 20*time.Millisecond)
	defer ts.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "cancel.bin")

	d := New(Options{StateStore: &state.FileStore{}})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	_, err := d.Run(ctx, Request{URL: ts.URL + "/cancel.bin", Dest: dest, Threads: 2, Resume: true})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("lỗi = %v, muốn ErrCancelled", err)
	}
	if _, statErr := os.Stat(state.PathFor(dest)); statErr != nil {
		t.Fatal("phải còn file trạng thái để resume sau khi huỷ")
	}
}

func TestChecksumMismatch(t *testing.T) {
	data := payload(10_000)
	ts := rangeServer(t, data)
	dir := t.TempDir()
	dest := filepath.Join(dir, "sum.bin")

	bad := sha256.Sum256([]byte("khác"))
	spec := &verify.Spec{Algorithm: verify.SHA256, Value: hex.EncodeToString(bad[:])}
	d := newDL(t)
	if _, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/sum.bin", Dest: dest, Threads: 2, Checksum: spec,
	}); err == nil {
		t.Fatal("muốn lỗi checksum")
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file đích không được công bố khi checksum sai")
	}
}

func TestRetryOnFlakyServer(t *testing.T) {
	data := payload(150_000)
	var failures atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead && failures.Add(1) <= 2 {
			// Cắt kết nối ngay để buộc client thử lại.
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(500)
				return
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("ETag", `"etag-1"`)
		http.ServeContent(w, r, "flaky.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer ts.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "flaky.bin")
	d := newDL(t)
	if _, err := d.Run(context.Background(), Request{
		URL: ts.URL + "/flaky.bin", Dest: dest, Threads: 2, Retries: 4, Resume: true,
	}); err != nil {
		t.Fatalf("Run: %v (retry phải cứu được)", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("nội dung sau retry sai")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

var _ io.Reader
