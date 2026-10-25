package segments

import "testing"

func TestPlanCoversWholeFile(t *testing.T) {
	cases := []struct {
		size  int64
		count int
	}{
		{1000, 3}, {1, 1}, {100, 8}, {1 << 20, 16}, {7, 32},
	}
	for _, c := range cases {
		segs, err := Plan(c.size, c.count)
		if err != nil {
			t.Fatalf("Plan(%d,%d): %v", c.size, c.count, err)
		}
		var covered int64
		next := int64(0)
		for _, s := range segs {
			if s.Start != next {
				t.Fatalf("size=%d count=%d: khoảng trống/chồng tại %d (muốn %d)", c.size, c.count, s.Start, next)
			}
			if s.Next != s.Start {
				t.Fatalf("Next khởi tạo phải = Start")
			}
			covered += s.End - s.Start + 1
			next = s.End + 1
		}
		if covered != c.size || next != c.size {
			t.Fatalf("size=%d count=%d: phủ %d, kết thúc %d", c.size, c.count, covered, next)
		}
	}
}

func TestManagerAssignsEveryByteOnce(t *testing.T) {
	const size = 4096
	segs, err := Plan(size, 4)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(segs, 256, 0)
	seen := make(map[int64]int, size)
	workers := []int{1, 2, 3, 4}
	active := len(workers)
	for active > 0 {
		active = 0
		for _, w := range workers {
			chunk, ok := m.Next(w)
			if !ok {
				m.Release(w)
				continue
			}
			active++
			for off := chunk.Start; off <= chunk.End; off++ {
				seen[off]++
			}
		}
	}
	if len(seen) != size {
		t.Fatalf("phủ %d byte, muốn %d", len(seen), size)
	}
	for off, n := range seen {
		if n != 1 {
			t.Fatalf("offset %d bị cấp %d lần", off, n)
		}
	}
	if !m.Complete() {
		t.Fatal("Complete() = false sau khi cấp hết")
	}
}

func TestManagerStealSplitsTail(t *testing.T) {
	// Hai segment lớn; worker 1 lấy segment 1, worker 2 phải steal được đuôi.
	segs, err := Plan(64*1024*4, 2)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(segs, 1024, 1024) // chunk nhỏ, ngưỡng steal thấp
	if _, ok := m.Next(1); !ok {
		t.Fatal("worker 1 không nhận được việc")
	}
	chunk, ok := m.Next(2)
	if !ok {
		t.Fatal("worker 2 không nhận được việc (không steal được)")
	}
	if chunk.Stolen {
		// Lần đầu worker 2 nhận segment 2 chưa có chủ; steal xảy ra sau đó.
	} else if chunk.SegID == 2 {
		// Tiếp tục xin việc để kích hoạt steal.
		found := false
		for i := 0; i < 200 && !found; i++ {
			c, ok := m.Next(2)
			if !ok {
				break
			}
			if c.Stolen {
				found = true
			}
		}
		if !found {
			t.Fatal("không quan sát được lần steal nào")
		}
	}
	// Kiểm tra không chồng lấn sau khi steal.
	seen := map[int64]bool{}
	for _, s := range m.Snapshot() {
		for off := s.Start; off <= s.End; off++ {
			if seen[off] {
				t.Fatalf("offset %d thuộc hai segment sau khi steal", off)
			}
			seen[off] = true
		}
	}
}

func TestPlanRejectsEmpty(t *testing.T) {
	if _, err := Plan(0, 4); err == nil {
		t.Fatal("muốn lỗi với size = 0")
	}
}
