/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

// Package broadcast is a small, dependency-free engine that delivers one
// message to many targets at a controlled rate.
//
// It exists to make long broadcasts (tens of thousands of targets) finish in a
// predictable time without starving the rest of the bot:
//
//   - a shared rate limiter keeps the whole broadcast at N sends/sec, leaving
//     headroom under Telegram's per-bot limit for normal bot traffic;
//   - a few workers overlap network latency so the limiter, not round-trip
//     time, decides the speed;
//   - a flood wait pauses every worker together, lowers the rate, and retries
//     the same target instead of dropping it;
//   - a panic while handling one target is contained to that target;
//   - a contiguous "watermark" records how far the broadcast has safely got so
//     it can be resumed after a stop or a restart.
package broadcast

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Kind classifies the result of a failed send.
type Kind int

const (
	// Failed is any error that is logged but not retried.
	Failed Kind = iota
	// Dead means the target is permanently unreachable (blocked the bot, chat
	// gone, account deleted...). It is reported through OnDead so it can be
	// marked in the database and skipped in future broadcasts.
	Dead
	// Flood means Telegram asked us to slow down; the engine waits and retries.
	Flood
)

// Outcome is what Classify returns for a failed send.
type Outcome struct {
	Kind Kind
	// Wait is how long Telegram asked us to back off (Kind == Flood).
	Wait time.Duration
	// Tag is a caller-defined reason for Dead targets (e.g. "chat", "blocked").
	Tag string
}

// Bounds for the send rate, in messages per second.
const (
	MinRate = 0.5
	MaxRate = 25.0
)

const (
	defaultMaxRetries = 3
	floodDebounce     = 5 * time.Second
	recoverAfter      = 60 * time.Second
	floodCushion      = time.Second
)

// Config describes one broadcast run.
type Config struct {
	// Targets to send to. Use NormalizeTargets to sort/dedupe/resume-filter.
	Targets []int64
	// Rate is the starting send rate in messages/sec.
	Rate float64
	// Workers is the number of concurrent senders; 0 picks a value from Rate.
	Workers int
	// MaxRetries is how many times one target is retried after a flood wait.
	MaxRetries int
	// Send delivers the message to one target.
	Send func(ctx context.Context, id int64) error
	// Classify decides what a send error means.
	Classify func(id int64, err error) Outcome
	// OnDead is called (from a worker goroutine) for each dead target.
	OnDead func(id int64, tag string)
	// OnFailed is called (from a worker goroutine) for each failed target.
	OnFailed func(id int64, err error)
	// Logf is optional diagnostics output.
	Logf func(format string, args ...any)
}

// Stats is a point-in-time snapshot of a run.
type Stats struct {
	Total      int
	Processed  int
	SentChats  int64
	SentUsers  int64
	Dead       int64
	Failed     int64
	Retries    int64
	FloodWaits int64
	// Rate is the current effective rate (lower than the configured one while
	// recovering from a flood wait).
	Rate float64
	// Paused is true while every worker is held back by a flood wait.
	Paused bool
}

// Engine runs a broadcast. Create it with New and call Run once.
type Engine struct {
	cfg     Config
	targets []int64

	next atomic.Int64

	sentChats atomic.Int64
	sentUsers atomic.Int64
	dead      atomic.Int64
	failed    atomic.Int64
	retries   atomic.Int64
	floods    atomic.Int64
	processed atomic.Int64

	wmMu   sync.Mutex
	done   []bool
	prefix int

	limMu      sync.Mutex
	baseRate   float64
	rate       float64
	nextSlot   time.Time
	pauseUntil time.Time
	lastFlood  time.Time
	lastRaise  time.Time
}

// New builds an engine. The Targets slice is used as given.
func New(cfg Config) *Engine {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	rate := clampRate(cfg.Rate)
	if cfg.Workers <= 0 {
		cfg.Workers = autoWorkers(rate)
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Engine{
		cfg:      cfg,
		targets:  cfg.Targets,
		done:     make([]bool, len(cfg.Targets)),
		baseRate: rate,
		rate:     rate,
	}
}

func clampRate(r float64) float64 {
	switch {
	case r < MinRate:
		return MinRate
	case r > MaxRate:
		return MaxRate
	}
	return r
}

// autoWorkers picks enough workers to hide network latency at the given rate,
// but not so many that they matter for CPU: about one worker per two
// messages/sec, between 3 and 10.
func autoWorkers(rate float64) int {
	w := int(rate/2 + 0.999)
	if w < 3 {
		w = 3
	}
	if w > 10 {
		w = 10
	}
	return w
}

// NormalizeTargets returns ids sorted ascending with duplicates removed. If
// after is non-nil, only ids strictly greater than *after are kept, which is
// how a resumed broadcast skips what was already delivered.
func NormalizeTargets(ids []int64, after *int64) []int64 {
	out := make([]int64, len(ids))
	copy(out, ids)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })

	res := out[:0]
	for i, id := range out {
		if i > 0 && id == out[i-1] {
			continue
		}
		if after != nil && id <= *after {
			continue
		}
		res = append(res, id)
	}
	return res
}

// Run sends to every target and returns when they are all handled or ctx is
// cancelled. A target that was interrupted by cancellation is not counted as
// done, so it is delivered again on resume.
func (e *Engine) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < e.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.worker(ctx)
		}()
	}
	wg.Wait()
}

func (e *Engine) worker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		idx := int(e.next.Add(1) - 1)
		if idx >= len(e.targets) {
			return
		}
		if !e.safeHandle(ctx, e.targets[idx]) {
			return
		}
		e.markDone(idx)
	}
}

// safeHandle runs handle and converts a panic anywhere in it (Send, Classify or
// the callbacks) into a failed target, so one bad target can't take down the
// process. It reports whether the target reached a final state.
func (e *Engine) safeHandle(ctx context.Context, id int64) (finished bool) {
	defer func() {
		if r := recover(); r != nil {
			e.cfg.Logf("broadcast: panic while handling %d: %v\n%s", id, r, debug.Stack())
			e.failed.Add(1)
			finished = true
		}
	}()
	return e.handle(ctx, id)
}

func (e *Engine) handle(ctx context.Context, id int64) bool {
	for attempt := 0; ; attempt++ {
		if err := e.wait(ctx); err != nil {
			return false
		}

		err := e.cfg.Send(ctx, id)
		if err == nil {
			if id < 0 {
				e.sentChats.Add(1)
			} else {
				e.sentUsers.Add(1)
			}
			return true
		}
		if ctx.Err() != nil {
			// The send failed because we are stopping; leave it undone.
			return false
		}

		out := e.cfg.Classify(id, err)
		switch out.Kind {
		case Flood:
			e.flood(out.Wait)
			if attempt >= e.cfg.MaxRetries {
				e.failed.Add(1)
				if e.cfg.OnFailed != nil {
					e.cfg.OnFailed(id, fmt.Errorf("still rate limited after %d retries: %w", attempt, err))
				}
				return true
			}
			e.retries.Add(1)
			continue
		case Dead:
			e.dead.Add(1)
			if e.cfg.OnDead != nil {
				e.cfg.OnDead(id, out.Tag)
			}
			return true
		default:
			e.failed.Add(1)
			if e.cfg.OnFailed != nil {
				e.cfg.OnFailed(id, err)
			}
			return true
		}
	}
}

// wait blocks until this worker may send: it takes the next slot from the
// shared limiter, then honours any flood pause that started in the meantime.
func (e *Engine) wait(ctx context.Context) error {
	slot := e.reserve()
	if err := sleepUntil(ctx, slot); err != nil {
		return err
	}
	for {
		e.limMu.Lock()
		p := e.pauseUntil
		e.limMu.Unlock()
		if !time.Now().Before(p) {
			return nil
		}
		if err := sleepUntil(ctx, p); err != nil {
			return err
		}
	}
}

func sleepUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reserve hands out the next send slot, spaced 1/rate apart and never earlier
// than the end of a flood pause.
func (e *Engine) reserve() time.Time {
	e.limMu.Lock()
	defer e.limMu.Unlock()

	now := time.Now()
	e.maybeRecoverLocked(now)

	slot := e.nextSlot
	if slot.Before(now) {
		slot = now
	}
	if slot.Before(e.pauseUntil) {
		slot = e.pauseUntil
	}
	e.nextSlot = slot.Add(time.Duration(float64(time.Second) / e.rate))
	return slot
}

// maybeRecoverLocked slowly climbs back to the configured rate after a flood
// wait once things have been quiet for a while.
func (e *Engine) maybeRecoverLocked(now time.Time) {
	if e.rate >= e.baseRate {
		return
	}
	if now.Sub(e.lastFlood) < recoverAfter || now.Sub(e.lastRaise) < recoverAfter {
		return
	}
	e.rate = min(e.baseRate, e.rate*1.25)
	e.lastRaise = now
	e.cfg.Logf("broadcast: rate recovering to %.1f/s", e.rate)
}

// flood pauses every worker for wait (plus a small cushion) and lowers the
// rate. Several workers usually hit the same flood at once, so the rate is
// only reduced once per floodDebounce window.
func (e *Engine) flood(wait time.Duration) {
	e.limMu.Lock()
	defer e.limMu.Unlock()

	now := time.Now()
	until := now.Add(wait + floodCushion)
	if until.After(e.pauseUntil) {
		e.pauseUntil = until
	}
	if now.Sub(e.lastFlood) > floodDebounce {
		e.rate = max(e.rate*0.7, MinRate)
		e.floods.Add(1)
		e.cfg.Logf("broadcast: flood wait %s, pausing and lowering rate to %.1f/s", wait, e.rate)
	}
	e.lastFlood = now
	e.lastRaise = now
}

// SetRate changes the target rate while the broadcast is running.
func (e *Engine) SetRate(r float64) float64 {
	r = clampRate(r)
	e.limMu.Lock()
	defer e.limMu.Unlock()
	e.baseRate = r
	e.rate = r
	return r
}

func (e *Engine) markDone(idx int) {
	e.wmMu.Lock()
	e.done[idx] = true
	for e.prefix < len(e.done) && e.done[e.prefix] {
		e.prefix++
	}
	e.wmMu.Unlock()
	e.processed.Add(1)
}

// Watermark returns the highest target ID such that it and every target before
// it (in ascending order) have been fully handled. ok is false while nothing
// has been completed in order yet. Resuming with NormalizeTargets(..., &id)
// continues exactly after it; targets finished out of order beyond the
// watermark may be sent again, which is the safe direction to err in.
func (e *Engine) Watermark() (id int64, ok bool) {
	e.wmMu.Lock()
	defer e.wmMu.Unlock()
	if e.prefix == 0 {
		return 0, false
	}
	return e.targets[e.prefix-1], true
}

// Stats returns a snapshot of the run.
func (e *Engine) Stats() Stats {
	e.limMu.Lock()
	rate := e.rate
	paused := time.Now().Before(e.pauseUntil)
	e.limMu.Unlock()

	return Stats{
		Total:      len(e.targets),
		Processed:  int(e.processed.Load()),
		SentChats:  e.sentChats.Load(),
		SentUsers:  e.sentUsers.Load(),
		Dead:       e.dead.Load(),
		Failed:     e.failed.Load(),
		Retries:    e.retries.Load(),
		FloodWaits: e.floods.Load(),
		Rate:       rate,
		Paused:     paused,
	}
}
