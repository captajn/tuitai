package category

import (
	"path/filepath"
	"testing"
)

func TestKindByExtension(t *testing.T) {
	c := New(`D:\DL`, true, nil)
	cases := map[string]Kind{
		"a.mp4":    Video,
		"b.MKV":    Video,
		"c.mp3":    Music,
		"d.pdf":    Documents,
		"e.zip":    Compressed,
		"f.exe":    Programs,
		"g.jpg":    Images,
		"h.unknow": Other,
		"noext":    Other,
	}
	for name, want := range cases {
		if got := c.KindOf(name, ""); got != want {
			t.Errorf("KindOf(%q) = %s, muốn %s", name, got, want)
		}
	}
}

func TestKindByMIME(t *testing.T) {
	c := New(`D:\DL`, true, nil)
	if got := c.KindOf("download", "video/mp4"); got != Video {
		t.Errorf("mime video/mp4 → %s", got)
	}
	if got := c.KindOf("download", "audio/mpeg"); got != Music {
		t.Errorf("mime audio/mpeg → %s", got)
	}
	if got := c.KindOf("download", "application/zip"); got != Compressed {
		t.Errorf("mime zip → %s", got)
	}
	if got := c.KindOf("download", "application/vnd.android.package-archive"); got != Programs {
		t.Errorf("mime apk → %s", got)
	}
}

func TestDirLayout(t *testing.T) {
	c := New(`D:\DL`, true, nil)
	got := c.Dir("phim.mp4", "")
	want := filepath.Join(`D:\DL`, "Video")
	if got != want {
		t.Fatalf("Dir = %q, muốn %q", got, want)
	}
	if got := c.Dir("la.xyz", ""); got != `D:\DL` {
		t.Fatalf("loại other phải nằm ở thư mục gốc, nhận %q", got)
	}
}

func TestDirDisabled(t *testing.T) {
	c := New(`D:\DL`, false, nil)
	if got := c.Dir("phim.mp4", ""); got != `D:\DL` {
		t.Fatalf("tắt phân loại nhưng Dir = %q", got)
	}
}

func TestOverrideFolder(t *testing.T) {
	c := New(`D:\DL`, true, map[string]string{"video": "Phim", "music": "Nhac"})
	if got, want := c.Dir("a.mp4", ""), filepath.Join(`D:\DL`, "Phim"); got != want {
		t.Fatalf("Dir = %q, muốn %q", got, want)
	}
	if got, want := c.Dir("a.mp3", ""), filepath.Join(`D:\DL`, "Nhac"); got != want {
		t.Fatalf("Dir = %q, muốn %q", got, want)
	}
}
