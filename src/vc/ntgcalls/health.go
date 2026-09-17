package ntgcalls

import (
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Per-engine health tracking (circuit breaker)
// ---------------------------------------------------------------------------
//
// Each *Client wraps one native ntgcalls engine, shared by every chat routed
// to that assistant. When the engine wedges, the old code had no notion of
// this: every chat kept issuing calls, each one burned the full 15s timeout
// and then abandoned its Future. With 60-70 live chats that is a steady
// stream of doomed calls, which is what turned a single stuck engine into a
// thousand parked goroutines.
//
// A circuit breaker turns that into a cheap, immediate error. After
// unhealthyThreshold consecutive timeouts the engine is marked unresponsive
// and further calls fail instantly, except for one probe every probeInterval
// so a recovering engine can close the circuit again.

var (
	// ErrNativeTimeout is returned when a native call does not complete
	// within its timeout.
	ErrNativeTimeout = errors.New("ntgcalls: native call timed out, engine appears unresponsive")

	// ErrEngineUnhealthy is returned without issuing a call at all, because
	// this engine has already been judged unresponsive. Callers should treat
	// it like ErrNativeTimeout - move the chat to another assistant - but it
	// costs nothing instead of 15 seconds.
	ErrEngineUnhealthy = errors.New("ntgcalls: native engine is unresponsive, call not attempted")
)

const (
	// unhealthyThreshold is how many consecutive timeouts mark an engine bad.
	// Kept low: a healthy engine answers bookkeeping calls in milliseconds, so
	// three 15s timeouts in a row is not bad luck.
	unhealthyThreshold = 3

	// probeInterval is how often a circuit-open engine is allowed one real
	// call through, to see whether it has recovered.
	probeInterval = 30 * time.Second
)

type engineHealth struct {
	mu                  sync.Mutex
	consecutiveTimeouts int
	open                bool // circuit open == engine considered dead
	openedAt            time.Time
	lastProbe           time.Time
	totalTimeouts       uint64
}

// begin is called before issuing a native call. A non-nil return means the
// call should be skipped entirely.
func (h *engineHealth) begin() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.open {
		return nil
	}

	now := time.Now()
	if now.Sub(h.lastProbe) >= probeInterval {
		h.lastProbe = now
		return nil // let this one through as a probe
	}
	return ErrEngineUnhealthy
}

// success records a completed native call and closes the circuit.
func (h *engineHealth) success() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveTimeouts = 0
	if h.open {
		h.open = false
		loggerNTGCalls.Info("native engine recovered, resuming normal operation")
	}
}

// timeout records a timed-out native call and may open the circuit.
func (h *engineHealth) timeout() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveTimeouts++
	h.totalTimeouts++
	if !h.open && h.consecutiveTimeouts >= unhealthyThreshold {
		h.open = true
		h.openedAt = time.Now()
		h.lastProbe = h.openedAt
		loggerNTGCalls.Error("native engine marked unresponsive after consecutive timeouts; failing fast until it recovers")
	}
}

// Healthy reports whether this engine is currently considered responsive.
// The vc layer uses it to route new chats away from a dead assistant instead
// of discovering the problem one 15-second timeout at a time.
func (ctx *Client) Healthy() bool {
	ctx.health.mu.Lock()
	defer ctx.health.mu.Unlock()
	return !ctx.health.open
}

// TimeoutCount reports how many native calls have timed out on this engine
// over its lifetime, for /stats style reporting.
func (ctx *Client) TimeoutCount() uint64 {
	ctx.health.mu.Lock()
	defer ctx.health.mu.Unlock()
	return ctx.health.totalTimeouts
}

// UnhealthySince reports when the circuit opened, or the zero time if the
// engine is healthy.
func (ctx *Client) UnhealthySince() time.Time {
	ctx.health.mu.Lock()
	defer ctx.health.mu.Unlock()
	if !ctx.health.open {
		return time.Time{}
	}
	return ctx.health.openedAt
}
