package ntgcalls

import (
	"sync"
	"sync/atomic"
	"time"
)

// #include "ntgcalls.h"
// extern void unlockMutex(void*);
import "C"
import (
	"unsafe"
)

type Future struct {
	mutex      *sync.Mutex
	errCode    *C.int
	errMessage **C.char
	// resolved flips to true the moment the native promise callback fires
	// (unlockMutex), even if nobody is blocked in WaitTimeout at that
	// moment - lets a caller check after the fact whether a timed-out call
	// eventually came back.
	resolved atomic.Bool
}

func CreateFuture() *Future {
	res := &Future{
		mutex:      &sync.Mutex{},
		errCode:    new(C.int),
		errMessage: new(*C.char),
	}
	res.mutex.Lock()
	return res
}

func (ctx *Future) ParseToC() C.ntg_async_struct {
	var x C.ntg_async_struct
	x.userData = unsafe.Pointer(ctx.mutex)
	x.promise = (C.ntg_async_callback)(unsafe.Pointer(C.unlockMutex))
	x.errorCode = (*C.int)(unsafe.Pointer(ctx.errCode))
	x.errorMessage = ctx.errMessage
	return x
}

// WaitTimeout waits up to d for the native promise to resolve, returning
// true if it completed in time and false otherwise.
//
// Background: ntg_stop/ntg_calls/etc. return immediately in C and complete
// asynchronously by invoking unlockMutex from a native worker thread. If
// that thread ever deadlocks (e.g. two overlapping native calls racing on
// the same chat inside the engine, or a WebRTC teardown that never signals
// completion), unlockMutex simply never fires again for THAT *Client -
// every later call that shares the same underlying engine blocks on
// ctx.mutex.Lock() forever, one goroutine at a time, with nothing ever
// surfaced as an error. That matches production symptoms exactly: dozens of
// goroutines parked in "sync.Mutex.Lock" for 10-150+ minutes inside
// future.go, all hanging off one native Client pointer, while every group
// assigned to that assistant silently stops working until the process
// eventually crashes outright.
//
// WaitTimeout can't safely kill the underlying native call - the goroutine
// spawned here is intentionally left running (and leaked) if we time out,
// since abandoning ctx.mutex mid-lock would corrupt it for whoever the
// native side eventually unlocks it for. What this buys us is turning an
// infinite silent hang into a bounded, observable failure, so callers can
// surface an error to the user and mark the engine unhealthy instead of
// freezing forever.
func (ctx *Future) WaitTimeout(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		ctx.mutex.Lock()
		ctx.resolved.Store(true)
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// Resolved reports whether the native promise has completed - useful for
// logging after a WaitTimeout(false) to see if it ever came back.
func (ctx *Future) Resolved() bool {
	return ctx.resolved.Load()
}

//export unlockMutex
func unlockMutex(p unsafe.Pointer) {
	m := (*sync.Mutex)(p)
	m.Unlock()
}
