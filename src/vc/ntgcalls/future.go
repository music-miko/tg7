package ntgcalls

/*
#include "ntgcalls.h"
#include <stdlib.h>
extern void unlockMutex(void*);
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Why this file was rewritten
// ---------------------------------------------------------------------------
//
// The previous implementation modelled a native promise as a locked
// sync.Mutex: CreateFuture() locked it, the native worker thread unlocked it
// from C (unlockMutex), and the Go caller blocked in mutex.Lock() until that
// happened. WaitTimeout() then layered a timeout on top by spawning a
// goroutine that did the blocking Lock() and reporting failure if it did not
// finish in time.
//
// That design had three defects, and together they explain the production
// symptoms (goroutines climbing past 1000 once ~60-70 voice chats are live,
// followed by "ntg_calls timed out after 15s"):
//
//  1. A goroutine per native call. Every single ntg_* call allocated a
//     goroutine purely to block on a mutex. On the happy path it exited
//     immediately, but on timeout it was deliberately abandoned - parked
//     forever in sync.Mutex.Lock() with an 8 KB stack. Once an engine starts
//     timing out, every subsequent call adds another permanent goroutine, so
//     the count only ever grows.
//
//  2. Go memory handed to C for asynchronous writes. errCode/errMessage were
//     allocated with new(C.int) - Go heap. Out-parameters like ntg_calls'
//     &buffer/&size were Go *stack* variables. Because these calls complete
//     asynchronously, the native engine writes through those pointers after
//     the Go function has already returned and its frame has been reused.
//     That is a straight violation of the cgo pointer rules and corrupts
//     whatever now occupies that memory - a very plausible reason the engine
//     degrades into unresponsiveness over hours rather than failing cleanly.
//
//  3. Nothing was ever freed on the timeout path. The native result buffer,
//     the error message string and the C strings passed in were all leaked.
//
// The replacement keeps the same C ABI (still one void* userData plus the
// unlockMutex callback, so ntgcalls.h is untouched) but:
//
//   - signals completion over a channel, so WaitTimeout needs no goroutine at
//     all and abandoning a call costs one map entry instead of a goroutine;
//   - allocates the control block, the error slots and every out-parameter in
//     C memory, so the native side is always writing into memory that stays
//     valid for as long as it might touch it;
//   - tracks every C allocation belonging to a call and frees them together,
//     but only once it is certain the native side is finished with them.
//
// The userData pointer is a genuine C-heap allocation (not a Go pointer, and
// not an integer cast to unsafe.Pointer), which keeps this legal under the
// cgo pointer rules and clean under `go vet`.

// futureRegistry maps the C-heap handle we hand to the native engine back to
// the Go-side Future. The native callback only ever receives that opaque
// pointer, so this is how it finds its Future again.
var futureRegistry = struct {
	sync.Mutex
	m map[uintptr]*Future
}{m: make(map[uintptr]*Future)}

// Future represents one in-flight asynchronous native call.
type Future struct {
	// handle is a 1-byte C allocation whose address uniquely identifies this
	// Future. It is passed to the engine as ntg_async_struct.userData.
	handle unsafe.Pointer

	// errCode / errMessage are the native error out-parameters. Both live in
	// C memory because the engine writes to them asynchronously.
	errCode    *C.int
	errMessage **C.char

	// done is closed when the native promise resolves, unless the call was
	// already abandoned by a timeout.
	done chan struct{}

	createdAt time.Time

	mu sync.Mutex
	// allocs holds every extra C allocation tied to this call (out-parameter
	// buffers, C strings for input arguments, media description structs).
	// They are freed together, and only when it is safe to do so.
	allocs []unsafe.Pointer
	// completed is set once the native callback has fired.
	completed bool
	// abandoned is set when a caller gave up waiting. After this point the
	// caller must not read any of the C memory, and the cleanup duty passes
	// to the native callback (or the reaper).
	abandoned bool
	// released is set once the C memory has been freed, to keep Release
	// idempotent.
	released bool
}

// CreateFuture allocates a Future and registers it so the native callback can
// find it later.
func CreateFuture() *Future {
	f := &Future{
		handle:     C.malloc(1),
		errCode:    (*C.int)(C.calloc(1, C.size_t(unsafe.Sizeof(C.int(0))))),
		errMessage: (**C.char)(C.calloc(1, C.size_t(unsafe.Sizeof((*C.char)(nil))))),
		done:       make(chan struct{}),
		createdAt:  time.Now(),
	}

	futureRegistry.Lock()
	futureRegistry.m[uintptr(f.handle)] = f
	futureRegistry.Unlock()

	return f
}

// ParseToC builds the ntg_async_struct the native call expects.
func (ctx *Future) ParseToC() C.ntg_async_struct {
	var x C.ntg_async_struct
	x.userData = ctx.handle
	x.promise = (C.ntg_async_callback)(unsafe.Pointer(C.unlockMutex))
	x.errorCode = ctx.errCode
	x.errorMessage = ctx.errMessage
	return x
}

// allocC returns n zeroed bytes of C memory owned by this Future. Use it for
// every out-parameter passed to an async native call, so the memory outlives
// the Go stack frame that started the call.
func (ctx *Future) allocC(n C.size_t) unsafe.Pointer {
	p := C.calloc(1, n)
	ctx.mu.Lock()
	ctx.allocs = append(ctx.allocs, p)
	ctx.mu.Unlock()
	return p
}

// cString copies s into C memory owned by this Future. This replaces bare
// C.CString() calls, which were never freed - notably the ffmpeg command line
// in every AudioDescription/VideoDescription, leaked on every single track
// change.
func (ctx *Future) cString(s string) *C.char {
	p := C.CString(s)
	ctx.mu.Lock()
	ctx.allocs = append(ctx.allocs, unsafe.Pointer(p))
	ctx.mu.Unlock()
	return p
}

// cBytes copies b into C memory owned by this Future.
func (ctx *Future) cBytes(b []byte) (*C.uint8_t, C.int) {
	if len(b) == 0 {
		return nil, 0
	}
	p := C.CBytes(b)
	ctx.mu.Lock()
	ctx.allocs = append(ctx.allocs, p)
	ctx.mu.Unlock()
	return (*C.uint8_t)(p), C.int(len(b))
}

// WaitTimeout waits up to d for the native promise to resolve. It returns
// true if the call completed in time.
//
// Unlike the previous implementation this spawns no goroutine: the native
// callback closes ctx.done directly, so a timeout costs nothing but a
// registry entry that the reaper will clear.
func (ctx *Future) WaitTimeout(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.done:
		return true
	case <-timer.C:
	}

	// The timer fired, but the callback may have landed in the same instant.
	// Take the lock and make the decision exactly once.
	ctx.mu.Lock()
	if ctx.completed {
		ctx.mu.Unlock()
		return true
	}
	ctx.abandoned = true
	ctx.mu.Unlock()
	return false
}

// Resolved reports whether the native promise has completed.
func (ctx *Future) Resolved() bool {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.completed
}

// Release unregisters the Future and frees all C memory it owns. Callers
// should `defer f.Release()` right after CreateFuture().
//
// It is a no-op for an abandoned call: the native engine may still be holding
// those pointers, so freeing them would be a use-after-free. Ownership in
// that case transfers to the native callback, or to the reaper.
func (ctx *Future) Release() {
	ctx.mu.Lock()
	if ctx.abandoned || ctx.released {
		ctx.mu.Unlock()
		return
	}
	ctx.released = true
	ctx.mu.Unlock()

	unregisterFuture(ctx)
	ctx.freeC()
}

// freeC frees every C allocation owned by this Future, including the native
// error message, which the engine allocates and previously nobody freed.
func (ctx *Future) freeC() {
	ctx.mu.Lock()
	allocs := ctx.allocs
	ctx.allocs = nil
	ctx.mu.Unlock()

	for _, p := range allocs {
		C.free(p)
	}

	if ctx.errMessage != nil {
		if msg := *ctx.errMessage; msg != nil {
			C.free(unsafe.Pointer(msg))
			*ctx.errMessage = nil
		}
		C.free(unsafe.Pointer(ctx.errMessage))
		ctx.errMessage = nil
	}
	if ctx.errCode != nil {
		C.free(unsafe.Pointer(ctx.errCode))
		ctx.errCode = nil
	}
	if ctx.handle != nil {
		C.free(ctx.handle)
		ctx.handle = nil
	}
}

func unregisterFuture(f *Future) {
	futureRegistry.Lock()
	delete(futureRegistry.m, uintptr(f.handle))
	futureRegistry.Unlock()
}

// complete is invoked from the native callback. If a caller is still waiting
// it wakes them; if the call was already abandoned it performs the deferred
// cleanup, since the native side is by definition finished with the memory at
// that point.
func (ctx *Future) complete() {
	ctx.mu.Lock()
	if ctx.completed {
		ctx.mu.Unlock()
		return
	}
	ctx.completed = true
	abandoned := ctx.abandoned
	if !abandoned {
		close(ctx.done)
	}
	ctx.mu.Unlock()

	if abandoned {
		// Late completion of a call somebody gave up on. Nobody will read the
		// results, and the engine is done writing, so reclaim everything now.
		unregisterFuture(ctx)
		ctx.freeC()
	}
}

//export unlockMutex
func unlockMutex(p unsafe.Pointer) {
	if p == nil {
		return
	}

	futureRegistry.Lock()
	f, ok := futureRegistry.m[uintptr(p)]
	futureRegistry.Unlock()

	if !ok {
		// Already reaped. Nothing sensible left to do; dropping the callback
		// is far better than touching freed memory.
		return
	}
	f.complete()
}

// ---------------------------------------------------------------------------
// Reaper
// ---------------------------------------------------------------------------

const (
	// futureReapAfter is how long an abandoned call may sit in the registry
	// before we stop expecting the engine to ever call back.
	futureReapAfter = 10 * time.Minute
	// futureReapInterval is how often the registry is swept.
	futureReapInterval = time.Minute
)

// PendingFutures reports how many native calls are currently registered -
// in-flight plus abandoned-but-not-yet-reaped. A number that climbs and never
// falls is the clearest signal that a native engine has wedged, and it is
// exported so /stats can surface it instead of waiting for the goroutine
// count to give it away.
func PendingFutures() int {
	futureRegistry.Lock()
	defer futureRegistry.Unlock()
	return len(futureRegistry.m)
}

func init() {
	go reapAbandonedFutures()
}

// reapAbandonedFutures drops registry entries for calls the engine has clearly
// forgotten about, so a permanently wedged engine leaks a bounded amount of
// memory instead of an unbounded number of map entries.
//
// The C memory for those entries is deliberately NOT freed: if the engine is
// merely very slow rather than dead, it may still write through those
// pointers, and a use-after-free is far worse than leaking ~32 bytes. A
// process in this state needs restarting anyway; the reaper exists to keep it
// limping usefully until then.
func reapAbandonedFutures() {
	ticker := time.NewTicker(futureReapInterval)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-futureReapAfter)
		var reaped int

		futureRegistry.Lock()
		for key, f := range futureRegistry.m {
			f.mu.Lock()
			stale := f.abandoned && !f.completed && f.createdAt.Before(cutoff)
			f.mu.Unlock()
			if stale {
				delete(futureRegistry.m, key)
				reaped++
			}
		}
		remaining := len(futureRegistry.m)
		futureRegistry.Unlock()

		if reaped > 0 {
			loggerNTGCalls.Warn(fmt.Sprintf(
				"reaped %d abandoned native call(s) that never completed; %d still registered",
				reaped, remaining,
			))
		}
	}
}
