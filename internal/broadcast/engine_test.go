package broadcast

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func ids(n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}

func defaultClassify(int64, error) Outcome { return Outcome{Kind: Failed} }

func TestNormalizeTargets(t *testing.T) {
	got := NormalizeTargets([]int64{5, -3, 5, 9, 1, -3}, nil)
	want := []int64{-3, 1, 5, 9}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	after := int64(1)
	got = NormalizeTargets([]int64{5, -3, 9, 1}, &after)
	if len(got) != 2 || got[0] != 5 || got[1] != 9 {
		t.Fatalf("resume filter: got %v", got)
	}
}

func TestSendsEveryTargetExactlyOnce(t *testing.T) {
	var mu sync.Mutex
	seen := map[int64]int{}
	e := New(Config{
		Targets:  ids(200),
		Rate:     MaxRate,
		Workers:  6,
		Classify: defaultClassify,
		Send: func(_ context.Context, id int64) error {
			mu.Lock()
			seen[id]++
			mu.Unlock()
			return nil
		},
	})
	e.Run(context.Background())

	for _, id := range ids(200) {
		if seen[id] != 1 {
			t.Fatalf("target %d sent %d times", id, seen[id])
		}
	}
	s := e.Stats()
	if s.Processed != 200 || s.SentUsers != 200 || s.SentChats != 0 {
		t.Fatalf("stats: %+v", s)
	}
	if id, ok := e.Watermark(); !ok || id != 200 {
		t.Fatalf("watermark = %d,%v", id, ok)
	}
}

func TestChatsAndUsersCountedBySign(t *testing.T) {
	e := New(Config{
		Targets:  NormalizeTargets([]int64{-100, -200, 7, 8, 9}, nil),
		Rate:     MaxRate,
		Classify: defaultClassify,
		Send:     func(context.Context, int64) error { return nil },
	})
	e.Run(context.Background())
	s := e.Stats()
	if s.SentChats != 2 || s.SentUsers != 3 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestRateIsRespected(t *testing.T) {
	// 31 sends at 50/s need 30 gaps of 20ms = 600ms.
	e := New(Config{
		Targets:  ids(31),
		Rate:     50,
		Workers:  8,
		Classify: defaultClassify,
		Send:     func(context.Context, int64) error { return nil },
	})
	start := time.Now()
	e.Run(context.Background())
	if el := time.Since(start); el < 550*time.Millisecond {
		t.Fatalf("finished in %s, limiter not pacing", el)
	}
}

func TestFloodPausesAllAndRetriesSameTarget(t *testing.T) {
	var floodedAt atomic.Int64
	var flooded atomic.Bool
	var mu sync.Mutex
	sends := map[int64]int{}
	var early atomic.Int32

	const wait = 400 * time.Millisecond
	e := New(Config{
		Targets: ids(40),
		Rate:    25,
		Workers: 5,
		Classify: func(id int64, err error) Outcome {
			return Outcome{Kind: Flood, Wait: wait}
		},
		Send: func(_ context.Context, id int64) error {
			// Any send that STARTS while the pause is active is a violation
			// (sends already in flight when the flood hit are not counted).
			if fa := floodedAt.Load(); fa != 0 {
				since := time.Since(time.Unix(0, fa))
				if since > 60*time.Millisecond && since < wait-40*time.Millisecond {
					early.Add(1)
				}
			}
			if id == 10 && flooded.CompareAndSwap(false, true) {
				floodedAt.Store(time.Now().UnixNano())
				return errBoom
			}
			mu.Lock()
			sends[id]++
			mu.Unlock()
			return nil
		},
	})
	e.Run(context.Background())

	if sends[10] != 1 {
		t.Fatalf("flooded target was not retried to success: %d", sends[10])
	}
	for _, id := range ids(40) {
		if sends[id] != 1 {
			t.Fatalf("target %d delivered %d times", id, sends[id])
		}
	}
	if n := early.Load(); n != 0 {
		t.Fatalf("%d sends started during the flood pause", n)
	}
	s := e.Stats()
	if s.Retries != 1 || s.FloodWaits != 1 || s.Failed != 0 {
		t.Fatalf("stats: %+v", s)
	}
	if s.Rate >= 25 {
		t.Fatalf("rate not lowered after flood: %v", s.Rate)
	}
}

func TestFloodGivesUpAfterMaxRetries(t *testing.T) {
	var failed atomic.Int32
	e := New(Config{
		Targets:    ids(1),
		Rate:       MaxRate,
		MaxRetries: 2,
		Classify:   func(int64, error) Outcome { return Outcome{Kind: Flood, Wait: time.Millisecond} },
		Send:       func(context.Context, int64) error { return errBoom },
		OnFailed:   func(int64, error) { failed.Add(1) },
	})
	e.Run(context.Background())
	s := e.Stats()
	if failed.Load() != 1 || s.Failed != 1 || s.Retries != 2 || s.Processed != 1 {
		t.Fatalf("stats: %+v failed=%d", s, failed.Load())
	}
}

func TestDeadTargetsReportedWithTag(t *testing.T) {
	var mu sync.Mutex
	got := map[int64]string{}
	e := New(Config{
		Targets: ids(10),
		Rate:    MaxRate,
		Classify: func(id int64, err error) Outcome {
			return Outcome{Kind: Dead, Tag: "blocked"}
		},
		Send: func(_ context.Context, id int64) error {
			if id%2 == 0 {
				return errBoom
			}
			return nil
		},
		OnDead: func(id int64, tag string) {
			mu.Lock()
			got[id] = tag
			mu.Unlock()
		},
	})
	e.Run(context.Background())
	if len(got) != 5 || got[2] != "blocked" {
		t.Fatalf("dead: %v", got)
	}
	if s := e.Stats(); s.Dead != 5 || s.SentUsers != 5 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestPanicIsContainedToOneTarget(t *testing.T) {
	e := New(Config{
		Targets:  ids(20),
		Rate:     MaxRate,
		Classify: defaultClassify,
		Send: func(_ context.Context, id int64) error {
			if id == 7 {
				panic("kaboom")
			}
			return nil
		},
	})
	e.Run(context.Background())
	s := e.Stats()
	if s.Failed != 1 || s.SentUsers != 19 || s.Processed != 20 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestPanicInCallbackIsContained(t *testing.T) {
	e := New(Config{
		Targets:  ids(5),
		Rate:     MaxRate,
		Classify: func(int64, error) Outcome { return Outcome{Kind: Dead} },
		Send:     func(context.Context, int64) error { return errBoom },
		OnDead:   func(int64, string) { panic("callback") },
	})
	e.Run(context.Background())
	if s := e.Stats(); s.Processed != 5 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestWatermarkNeverPassesAnUnfinishedTarget(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	sent := map[int64]bool{}
	e := New(Config{
		Targets:  ids(30),
		Rate:     MaxRate,
		Workers:  5,
		Classify: defaultClassify,
		Send: func(ctx context.Context, id int64) error {
			if id == 3 {
				select { // target 3 is slow
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			mu.Lock()
			sent[id] = true
			mu.Unlock()
			return nil
		},
	})
	go e.Run(context.Background())

	deadline := time.Now().Add(3 * time.Second)
	for e.Stats().Processed < 25 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.Stats().Processed < 25 {
		t.Fatalf("workers did not get past the slow target: %+v", e.Stats())
	}
	id, ok := e.Watermark()
	if !ok || id != 2 {
		t.Fatalf("watermark should stop at 2 while 3 is in flight, got %d,%v", id, ok)
	}
	close(release)
	deadline = time.Now().Add(3 * time.Second)
	for e.Stats().Processed < 30 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if id, _ := e.Watermark(); id != 30 {
		t.Fatalf("watermark should reach 30, got %d", id)
	}
}

func TestCancelThenResumeDeliversEverything(t *testing.T) {
	all := ids(120)
	var mu sync.Mutex
	delivered := map[int64]int{}
	send := func(_ context.Context, id int64) error {
		mu.Lock()
		delivered[id]++
		mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	e1 := New(Config{Targets: NormalizeTargets(all, nil), Rate: 100, Workers: 4, Classify: defaultClassify, Send: send})
	go func() {
		for e1.Stats().Processed < 40 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	e1.Run(ctx)

	wm, ok := e1.Watermark()
	if !ok {
		t.Fatal("no watermark after partial run")
	}
	if e1.Stats().Processed >= 120 {
		t.Fatal("run was not interrupted")
	}

	e2 := New(Config{Targets: NormalizeTargets(all, &wm), Rate: MaxRate, Workers: 4, Classify: defaultClassify, Send: send})
	e2.Run(context.Background())

	for _, id := range all {
		if delivered[id] < 1 {
			t.Fatalf("target %d never delivered (watermark %d)", id, wm)
		}
	}
	dups := 0
	for _, n := range delivered {
		if n > 1 {
			dups += n - 1
		}
	}
	if dups > 8 { // bounded by in-flight/out-of-order work, never a full re-send
		t.Fatalf("too many duplicates on resume: %d", dups)
	}
}

func TestSetRateClamps(t *testing.T) {
	e := New(Config{Targets: ids(1), Rate: 5, Classify: defaultClassify, Send: func(context.Context, int64) error { return nil }})
	if got := e.SetRate(1000); got != MaxRate {
		t.Fatalf("got %v", got)
	}
	if got := e.SetRate(0); got != MinRate {
		t.Fatalf("got %v", got)
	}
	if got := e.SetRate(12); got != 12 || e.Stats().Rate != 12 {
		t.Fatalf("got %v", got)
	}
}
