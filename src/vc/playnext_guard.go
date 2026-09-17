/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import "sync"

// ---------------------------------------------------------------------------
// Per-chat advance serialization
// ---------------------------------------------------------------------------
//
// OnStreamEnd fires from a native engine thread and kicks off PlayNext, which
// downloads a track and starts a new stream - seconds of work, much of it
// network I/O.
//
// Nothing stopped two of those running concurrently for the same chat. A chat
// whose stream keeps ending early (a dead CDN link, a track ffmpeg cannot
// open, a flapping connection) produces a burst of stream-end events, each
// spawning another PlayNext, each racing the others to mutate the same queue.
// Under load that is both a goroutine multiplier and a source of skipped or
// double-played tracks.
//
// advanceGuard admits one advance per chat at a time. A second event arriving
// while one is in flight is dropped rather than queued: by the time the
// in-flight advance finishes it will already have moved the queue on, so the
// dropped event has nothing left to do.

type advanceGuard struct {
	mu       sync.Mutex
	inFlight map[int64]bool
}

var chatAdvanceGuard = &advanceGuard{inFlight: make(map[int64]bool)}

// tryStart claims the advance slot for chatID, returning false if one is
// already running.
func (g *advanceGuard) tryStart(chatID int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight[chatID] {
		return false
	}
	g.inFlight[chatID] = true
	return true
}

// finish releases the advance slot for chatID.
func (g *advanceGuard) finish(chatID int64) {
	g.mu.Lock()
	delete(g.inFlight, chatID)
	g.mu.Unlock()
}

// InFlightAdvances reports how many chats are mid-advance, for /stats.
func InFlightAdvances() int {
	chatAdvanceGuard.mu.Lock()
	defer chatAdvanceGuard.mu.Unlock()
	return len(chatAdvanceGuard.inFlight)
}
