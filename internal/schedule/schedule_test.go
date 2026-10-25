package schedule

import (
	"context"
	"testing"
	"time"
)

func TestParseDaily(t *testing.T) {
	d, err := ParseDaily("01:00-07:30")
	if err != nil {
		t.Fatal(err)
	}
	if d.Start != 60 || d.End != 450 {
		t.Fatalf("Start=%d End=%d", d.Start, d.End)
	}
	if got := d.String(); got != "01:00-07:30" {
		t.Fatalf("String = %q", got)
	}
	for _, bad := range []string{"1h-2h", "25:00-26:00", "01:00", "05:00-05:00", "01:61-02:00"} {
		if _, err := ParseDaily(bad); err == nil {
			t.Errorf("ParseDaily(%q) muốn lỗi", bad)
		}
	}
	if d, err := ParseDaily(""); err != nil || !d.AllDay {
		t.Fatalf("chuỗi rỗng phải là AllDay, err=%v", err)
	}
}

func TestActiveSimpleWindow(t *testing.T) {
	d, _ := ParseDaily("01:00-07:00")
	loc := time.Local
	cases := []struct {
		h, m int
		want bool
	}{
		{0, 59, false}, {1, 0, true}, {6, 59, true}, {7, 0, false}, {23, 0, false},
	}
	for _, c := range cases {
		tm := time.Date(2026, 10, 25, c.h, c.m, 0, 0, loc)
		if got := d.Active(tm); got != c.want {
			t.Errorf("%02d:%02d → %v, muốn %v", c.h, c.m, got, c.want)
		}
	}
}

func TestActiveAcrossMidnight(t *testing.T) {
	d, _ := ParseDaily("22:00-06:00")
	loc := time.Local
	if !d.Active(time.Date(2026, 10, 25, 23, 30, 0, 0, loc)) {
		t.Fatal("23:30 phải nằm trong khung 22:00-06:00")
	}
	if !d.Active(time.Date(2026, 10, 25, 5, 0, 0, 0, loc)) {
		t.Fatal("05:00 phải nằm trong khung")
	}
	if d.Active(time.Date(2026, 10, 25, 12, 0, 0, 0, loc)) {
		t.Fatal("12:00 phải ngoài khung")
	}
}

func TestNextOpen(t *testing.T) {
	d, _ := ParseDaily("01:00-07:00")
	loc := time.Local
	// 09:00 → mở cửa 01:00 hôm sau.
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, loc)
	next := d.NextOpen(at)
	want := time.Date(2026, 10, 26, 1, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("NextOpen = %v, muốn %v", next, want)
	}
	// 00:30 → mở 01:00 cùng ngày.
	at2 := time.Date(2026, 10, 25, 0, 30, 0, 0, loc)
	if got := d.NextOpen(at2); !got.Equal(time.Date(2026, 10, 25, 1, 0, 0, 0, loc)) {
		t.Fatalf("NextOpen = %v", got)
	}
	// Đang mở → trả về chính nó.
	at3 := time.Date(2026, 10, 25, 3, 0, 0, 0, loc)
	if got := d.NextOpen(at3); !got.Equal(at3) {
		t.Fatalf("trong khung phải trả về nguyên giá trị, nhận %v", got)
	}
}

func TestGateWaitCancel(t *testing.T) {
	g, err := New("01:00-07:00")
	if err != nil {
		t.Fatal(err)
	}
	closed := time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Wait(ctx, func() time.Time { return closed }, 10*time.Millisecond); err != context.Canceled {
		t.Fatalf("muốn context.Canceled, nhận %v", err)
	}
}

func TestGateNilIsAlwaysOpen(t *testing.T) {
	var g *Gate
	if !g.Active(time.Now()) {
		t.Fatal("gate nil phải luôn mở")
	}
	if err := g.Wait(context.Background(), time.Now, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
