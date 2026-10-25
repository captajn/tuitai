package grab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleHTML = `<!doctype html>
<html><head>
  <meta property="og:video" content="/media/trailer.mp4">
  <link rel="stylesheet" href="/style.css">
</head><body>
  <a href="/files/bao-cao.pdf">Báo cáo quý 4</a>
  <a href="phim/hanh-dong.mkv">Phim hành động</a>
  <a href="/files/archive.zip">Tải ZIP</a>
  <a href="/trang-khac">Trang khác</a>
  <a href="#top">Lên đầu</a>
  <video src="/media/clip.webm"></video>
  <img src="icon.png">
  <a href="https://cdn.example.org/stream/master.m3u8">HLS</a>
  <script>var u = "https://cdn.example.org/song.mp3";</script>
</body></html>`

func TestExtractBasics(t *testing.T) {
	links, err := Extract(sampleHTML, "https://site.example/dir/page.html", Options{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]Link{}
	for _, l := range links {
		got[l.URL] = l
	}
	want := []string{
		"https://site.example/media/trailer.mp4",
		"https://site.example/style.css",
		"https://site.example/files/bao-cao.pdf",
		"https://site.example/dir/phim/hanh-dong.mkv",
		"https://site.example/files/archive.zip",
		"https://site.example/media/clip.webm",
		"https://site.example/dir/icon.png",
		"https://cdn.example.org/stream/master.m3u8",
		"https://cdn.example.org/song.mp3",
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("thiếu link %q", w)
		}
	}
	if l := got["https://site.example/files/bao-cao.pdf"]; l.Text != "Báo cáo quý 4" {
		t.Errorf("nhãn = %q", l.Text)
	}
	if l := got["https://site.example/media/trailer.mp4"]; l.Kind != "media" {
		t.Errorf("kind mp4 = %q", l.Kind)
	}
	if l := got["https://cdn.example.org/stream/master.m3u8"]; l.Kind != "playlist" {
		t.Errorf("kind m3u8 = %q", l.Kind)
	}
	// Mặc định bỏ link không có phần mở rộng (trang điều hướng).
	if _, ok := got["https://site.example/trang-khac"]; ok {
		t.Error("IncludePages=false nhưng vẫn giữ link trang")
	}
	for _, l := range links {
		if strings.HasPrefix(l.URL, "#") || strings.Contains(l.URL, "javascript:") {
			t.Errorf("không được giữ link rác: %q", l.URL)
		}
	}
}

func TestExtractIncludePages(t *testing.T) {
	links, err := Extract(sampleHTML, "https://site.example/dir/page.html", Options{IncludePages: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range links {
		if l.URL == "https://site.example/trang-khac" {
			found = true
			if l.Kind != "page" {
				t.Fatalf("kind = %q, muốn page", l.Kind)
			}
		}
	}
	if !found {
		t.Fatal("IncludePages=true phải giữ link trang")
	}
}

func TestExtractFilterExts(t *testing.T) {
	links, err := Extract(sampleHTML, "https://site.example/", Options{Exts: []string{"mp4", "m3u8"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("lọc còn %d link, muốn 2: %+v", len(links), links)
	}
	for _, l := range links {
		if l.Ext != "mp4" && l.Ext != "m3u8" {
			t.Fatalf("lọt link %q", l.URL)
		}
	}
}

func TestExtractRejectsEmpty(t *testing.T) {
	if _, err := Extract("   ", "https://x/", Options{}); err == nil {
		t.Fatal("muốn lỗi với HTML rỗng")
	}
	if _, err := Extract("<html><body>không có gì</body></html>", "https://x/", Options{}); err == nil {
		t.Fatal("muốn ErrNoLink")
	}
}

func TestExtractDedupes(t *testing.T) {
	html := `<a href="/a.mp4">1</a><a href="/a.mp4">2</a><video src="/a.mp4"></video>`
	links, err := Extract(html, "https://x/", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("có %d link, muốn 1 sau khi bỏ trùng", len(links))
	}
}

func TestPageFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<a href="/real.bin">tải</a>`))
	}))
	defer ts.Close()
	links, err := Page(context.Background(), ts.Client(), ts.URL+"/", Options{})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if len(links) != 1 || !strings.HasSuffix(links[0].URL, "/real.bin") {
		t.Fatalf("links = %+v", links)
	}
}

const masterPlaylist = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720
720p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=640000,RESOLUTION=640x360
360p/index.m3u8
`

const mediaPlaylist = `#EXTM3U
#EXT-X-TARGETDURATION:10
#EXTINF:9.9,
seg-1.ts
#EXTINF:9.9,
/tmp/seg-2.ts
#EXT-X-ENDLIST
`

func TestPlaylistVariants(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if strings.Contains(r.URL.Path, "master") {
			_, _ = w.Write([]byte(masterPlaylist))
			return
		}
		_, _ = w.Write([]byte(mediaPlaylist))
	}))
	defer ts.Close()

	links, err := Playlist(context.Background(), ts.Client(), ts.URL+"/hls/master.m3u8", Options{})
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("có %d variant, muốn 2", len(links))
	}
	if links[0].Kind != "playlist" || !strings.HasSuffix(links[0].URL, "/hls/720p/index.m3u8") {
		t.Fatalf("variant[0] = %+v", links[0])
	}

	segs, err := Playlist(context.Background(), ts.Client(), ts.URL+"/hls/720p/index.m3u8", Options{})
	if err != nil {
		t.Fatalf("Playlist segments: %v", err)
	}
	if len(segs) != 2 {
		t.Fatalf("có %d segment, muốn 2", len(segs))
	}
	if !strings.HasSuffix(segs[0].URL, "/hls/720p/seg-1.ts") {
		t.Fatalf("segment[0] = %+v", segs[0])
	}
	if !strings.HasSuffix(segs[1].URL, "/tmp/seg-2.ts") {
		t.Fatalf("segment tuyệt đối sai: %+v", segs[1])
	}
}
