package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRun trả về hàm Run chạy trong thời gian nhất định và ghi nhận thứ tự.
func fakeRun(order *[]string, mu *sync.Mutex, d time.Duration, fail bool) func(context.Context, *Job) error {
	return func(ctx context.Context, j *Job) error {
		mu.Lock()
		*order = append(*order, j.ID)
		mu.Unlock()
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
		if fail {
			return errors.New("lỗi giả lập")
		}
		j.SetProgress(100, 100)
		return nil
	}
}

func TestPriorityOrder(t *testing.T) {
	var (
		order []string
		mu    sync.Mutex
	)
	m := New(Options{MaxConcurrent: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	mk := func(id string, prio int) *Job {
		j := &Job{ID: id, URL: "http://x/" + id, Priority: prio, CreatedAt: time.Now()}
		j.Run = fakeRun(&order, &mu, 5*time.Millisecond, false)
		return j
	}
	_ = m.Add(mk("low", 0))
	_ = m.Add(mk("high", 10))
	_ = m.Add(mk("mid", 5))
	m.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("chạy %d job, muốn 3", len(order))
	}
	if order[0] != "high" || order[1] != "mid" || order[2] != "low" {
		t.Fatalf("thứ tự = %v, muốn [high mid low]", order)
	}
}

func TestMaxConcurrentLimit(t *testing.T) {
	var cur, peak atomic.Int32
	m := New(Options{MaxConcurrent: 2})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	for i := 0; i < 6; i++ {
		j := &Job{ID: string(rune('a' + i)), URL: "http://x/", CreatedAt: time.Now()}
		j.Run = func(ctx context.Context, j *Job) error {
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(15 * time.Millisecond)
			cur.Add(-1)
			return nil
		}
		if err := m.Add(j); err != nil {
			t.Fatal(err)
		}
	}
	m.Wait()
	if peak.Load() > 2 {
		t.Fatalf("chạy đồng thời %d job, vượt giới hạn 2", peak.Load())
	}
}

func TestPauseAndResume(t *testing.T) {
	m := New(Options{MaxConcurrent: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	started := make(chan struct{})
	var once sync.Once
	j := &Job{ID: "p1", URL: "http://x/", CreatedAt: time.Now()}
	j.Run = func(ctx context.Context, j *Job) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}
	if err := m.Add(j); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.Pause("p1"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	waitFor(t, func() bool { return j.State1() == StatePaused })
	if err := m.Resume("p1"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitFor(t, func() bool {
		s := j.State1()
		return s == StateActive || s == StateQueued || s == StatePaused
	})
	// Sau khi resume, manager sẽ chạy lại; huỷ để kết thúc.
	cancel()
	m.Close()
}

func TestFailedStateAndRemove(t *testing.T) {
	m := New(Options{MaxConcurrent: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Start(ctx)

	j := &Job{ID: "f1", URL: "http://x/", CreatedAt: time.Now()}
	j.Run = func(ctx context.Context, j *Job) error { return errors.New("hỏng") }
	if err := m.Add(j); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if j.State1() != StateFailed {
		t.Fatalf("trạng thái = %s, muốn failed", j.State1())
	}
	if err := m.Remove("f1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := m.Get("f1"); ok {
		t.Fatal("job vẫn còn sau khi Remove")
	}
}

func TestDuplicateIDRejected(t *testing.T) {
	m := New(Options{})
	j := &Job{ID: "dup", URL: "http://x/", CreatedAt: time.Now()}
	if err := m.Add(j); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(&Job{ID: "dup", URL: "http://y/", CreatedAt: time.Now()}); err == nil {
		t.Fatal("muốn lỗi khi trùng ID")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hết thời gian chờ điều kiện")
}
