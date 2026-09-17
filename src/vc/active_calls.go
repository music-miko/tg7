/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Local active-call bookkeeping
// ---------------------------------------------------------------------------
//
// Assistant.Play used to begin with:
//
//	if a.binding.Calls()[chatId] != nil { ... }
//
// just to answer "is this one chat already connected?". ntg_calls is an
// engine-wide operation: it takes an internal lock and marshals every active
// chat into a fresh map, so its cost scales with the number of live voice
// chats while the question being asked is about exactly one of them.
//
// That is why "ntg_calls timed out after 15s" is the first thing to break
// once ~60-70 chats are live - it is both the most expensive call and the one
// issued most often.
//
// The engine already tells us everything we need to maintain this set
// ourselves: we know when we successfully connect a chat and when we stop it.
// So we track it locally and answer from memory. Two safeguards keep the set
// honest:
//
//   - If the local set says "active" but the engine disagrees, SetStreamSources
//     returns a "not found" error; Play treats that as a miss, clears the entry
//     and falls through to a full connect. Drift costs one wasted call, never a
//     stuck chat.
//   - A slow reconciler re-syncs against the engine periodically, which also
//     doubles as a liveness probe: if ntg_calls times out there, the circuit
//     breaker opens and the assistant is taken out of rotation before any user
//     hits it.

// markCallActive records that chatId is connected on this assistant.
func (a *Assistant) markCallActive(chatId int64) {
	a.mu.Lock()
	if a.activeCalls == nil {
		a.activeCalls = make(map[int64]struct{})
	}
	a.activeCalls[chatId] = struct{}{}
	a.mu.Unlock()
}

// markCallInactive forgets chatId.
func (a *Assistant) markCallInactive(chatId int64) {
	a.mu.Lock()
	delete(a.activeCalls, chatId)
	a.mu.Unlock()
}

// isCallActive reports whether we believe chatId is currently connected.
func (a *Assistant) isCallActive(chatId int64) bool {
	a.mu.RLock()
	_, ok := a.activeCalls[chatId]
	a.mu.RUnlock()
	return ok
}

// ActiveCallCount returns how many chats this assistant believes it is
// streaming to. Cheap - no native call - so it is safe for /stats.
func (a *Assistant) ActiveCallCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.activeCalls)
}

// Healthy reports whether this assistant's native engine is responsive.
func (a *Assistant) Healthy() bool {
	return a.binding.Healthy()
}

// isCallNotFound reports whether err means "the engine has no such call",
// which is how a stale entry in our local set surfaces.
func isCallNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "connection not found")
}

const (
	// activeCallSyncInterval is how often the local set is reconciled against
	// the engine. Deliberately slow: this is drift correction and a liveness
	// probe, not the hot path.
	activeCallSyncInterval = 5 * time.Minute

	// activeCallSyncTimeout bounds the reconcile call. Shorter than the
	// default native timeout because nothing is waiting on it and a slow
	// answer is itself the signal we care about.
	activeCallSyncTimeout = 10 * time.Second
)

// startActiveCallSync runs the periodic reconcile for this assistant.
func (a *Assistant) startActiveCallSync() {
	go func() {
		ticker := time.NewTicker(activeCallSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			a.syncActiveCalls()
		}
	}()
}

// syncActiveCalls replaces the local set with the engine's own view. A
// failure here is left alone rather than clearing the set: an unreachable
// engine tells us nothing about which chats are connected, and wiping the set
// would make every chat attempt a redundant full reconnect.
func (a *Assistant) syncActiveCalls() {
	calls, err := a.binding.CallsTimeout(activeCallSyncTimeout)
	if err != nil {
		logger.Warn("[ActiveCallSync] could not reconcile with native engine, keeping local view",
			"error", err, "healthy", a.binding.Healthy())
		return
	}

	next := make(map[int64]struct{}, len(calls))
	for chatId := range calls {
		next[chatId] = struct{}{}
	}

	a.mu.Lock()
	before := len(a.activeCalls)
	a.activeCalls = next
	a.mu.Unlock()

	if before != len(next) {
		logger.Info("[ActiveCallSync] reconciled active calls with native engine",
			"before", before, "after", len(next))
	}
}
