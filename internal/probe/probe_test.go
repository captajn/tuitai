package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestProbeRangeServer(t *testing.T) {
	body := make([]byte, 4096)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Header().Set("Content-Disposition", `attachment; filename="ban-phim.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		http.ServeContent(w, r, "ban-phim.zip", timeZero(), readerOf(body))
	}))
	defer ts.Close()

	p := New(ts.Client(), Options{})
	res, err := p.Probe(context.Background(), ts.URL+"/file.zip")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if res.Size != int64(len(body)) {
		t.Fatalf("Size = %d, muốn %d", res.Size, len(body))
	}
	if !res.RangeOK {
		t.Fatal("RangeOK = false, muốn true (http.ServeContent hỗ trợ Range)")
	}
	if res.ETag != "v1" {
		t.Fatalf("ETag = %q", res.ETag)
	}
	if res.Filename != "ban-phim.zip" {
		t.Fatalf("Filename = %q", res.Filename)
	}
	if res.MIME != "application/zip" {
		t.Fatalf("MIME = %q", res.MIME)
	}
}

func TestProbeNoRange(t *testing.T) {
	body := []byte("hello world")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Type", "text/plain")
		w.Write(body)
	}))
	defer ts.Close()

	p := New(ts.Client(), Options{})
	res, err := p.Probe(context.Background(), ts.URL+"/a.txt")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if res.RangeOK {
		t.Fatal("RangeOK = true trên server không hỗ trợ Range")
	}
	if res.Size != int64(len(body)) {
		t.Fatalf("Size = %d, muốn %d", res.Size, len(body))
	}
}

func TestProbeNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	p := New(ts.Client(), Options{})
	_, err := p.Probe(context.Background(), ts.URL+"/missing")
	if err == nil {
		t.Fatal("muốn lỗi 404")
	}
	var unsup ErrUnsupported
	if !asErr(err, &unsup) {
		t.Fatalf("lỗi phải là ErrUnsupported, nhận %T: %v", err, err)
	}
}

func TestProbeRejectsNonHTTP(t *testing.T) {
	p := New(nil, Options{})
	if _, err := p.Probe(context.Background(), "ftp://example.com/x"); err == nil {
		t.Fatal("muốn lỗi với giao thức không hỗ trợ")
	}
}

func TestProbeFallbackHeadToGet(t *testing.T) {
	body := make([]byte, 1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(w, r, "x.bin", timeZero(), readerOf(body))
	}))
	defer ts.Close()

	p := New(ts.Client(), Options{FallbackToGet: true})
	res, err := p.Probe(context.Background(), ts.URL+"/x.bin")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if res.Size != int64(len(body)) {
		t.Fatalf("Size = %d, muốn %d", res.Size, len(body))
	}
}
