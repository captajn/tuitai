package hls

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// segBody tạo dữ liệu nhận dạng được cho từng segment.
func segBody(i int) []byte {
	b := make([]byte, 1000+i)
	for j := range b {
		b[j] = byte(i)
	}
	return b
}

func TestParseAndDownload(t *testing.T) {
	const n = 5
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "master.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow/index.m3u8\n"))
		case strings.HasSuffix(r.URL.Path, "low/index.m3u8"):
			var b strings.Builder
			b.WriteString("#EXTM3U\n")
			for i := 0; i < n; i++ {
				b.WriteString("#EXTINF:1.0,\n")
				b.WriteString("seg-" + strconv.Itoa(i) + ".ts\n")
			}
			b.WriteString("#EXT-X-ENDLIST\n")
			w.Write([]byte(b.String()))
		default:
			idx, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(r.URL.Path), "seg-"), ".ts"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Write(segBody(idx))
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.ts")
	res, err := Run(context.Background(), ts.Client(), ts.URL+"/hls/master.m3u8", dest, Options{Threads: 3})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Segments != n {
		t.Fatalf("Segments = %d, muốn %d", res.Segments, n)
	}
	if res.Merged != "concat" {
		t.Fatalf("Merged = %q, muốn concat (không có ffmpeg trong test)", res.Merged)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for i := 0; i < n; i++ {
		want += len(segBody(i))
	}
	if len(got) != want {
		t.Fatalf("kích thước ghép %d, muốn %d", len(got), want)
	}
	// Kiểm tra thứ tự: các segment nối đúng thứ tự 0..n-1.
	off := 0
	for i := 0; i < n; i++ {
		if got[off] != byte(i) {
			t.Fatalf("byte đầu segment %d sai (0x%02x) tại offset %d", i, got[off], off)
		}
		off += len(segBody(i))
	}
}

func TestInitSegmentFirst(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "index.m3u8"):
			w.Write([]byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1,\na.m4s\n#EXTINF:1,\nb.m4s\n"))
		case strings.HasSuffix(r.URL.Path, "init.mp4"):
			w.Write([]byte("INIT"))
		case strings.HasSuffix(r.URL.Path, "a.m4s"):
			w.Write([]byte("AAA"))
		case strings.HasSuffix(r.URL.Path, "b.m4s"):
			w.Write([]byte("BBB"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "v.mp4")
	res, err := Run(context.Background(), ts.Client(), ts.URL+"/index.m3u8", dest, Options{Threads: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.InitSeg {
		t.Fatal("InitSeg = false")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "INITAAABBB" {
		t.Fatalf("nội dung = %q, muốn INITAAABBB", string(got))
	}
}

func TestNoSegment(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("#EXTM3U\n"))
	}))
	defer ts.Close()
	_, err := Run(context.Background(), ts.Client(), ts.URL+"/x.m3u8", filepath.Join(t.TempDir(), "x"), Options{})
	if err == nil {
		t.Fatal("muốn ErrNoSegment")
	}
}

func TestAttrParsing(t *testing.T) {
	if got := attr(`#EXT-X-MAP:URI="init.mp4",BYTERANGE="1@2"`, "URI"); got != "init.mp4" {
		t.Fatalf("attr URI = %q", got)
	}
	if got := attr("#EXT-X-STREAM-INF:URI=plain.m3u8,BANDWIDTH=1", "URI"); got != "plain.m3u8" {
		t.Fatalf("attr không nháy = %q", got)
	}
}

var _ = rand.Reader
