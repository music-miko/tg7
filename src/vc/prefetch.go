/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"sync"
	"time"

	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/core/dl"
	"ashokshau/tgmusic/src/utils"

	td "github.com/AshokShau/gotdbot"
)

// prefetchLeadTime is how long before the current track ends we start
// downloading the upcoming one, so it's already on disk by the time
// OnStreamEnd fires and PlayNext needs it - no gap, no "Downloading..."
// message for anything but the very first track in a queue.
const prefetchLeadTime = 30 * time.Second

// minPrefetchDelay is the floor for the prefetch delay, so very short
// tracks (<= prefetchLeadTime) still get a moment of playback before the
// download for the next one kicks off, rather than firing near-instantly.
const minPrefetchDelay = 3 * time.Second

// schedulePrefetch (re)schedules a background download of the chat's
// upcoming track to start prefetchLeadTime before durationSeconds elapses.
// Any previously scheduled prefetch for this chat is cancelled first, so
// skips/loops/track changes always end up with exactly one pending timer
// per chat, aimed at whatever is actually next when it fires.
func (c *TelegramCalls) schedulePrefetch(bot *td.Client, chatID int64, durationSeconds int) {
	c.cancelPrefetch(chatID)

	if durationSeconds <= 0 {
		// Unknown duration - fall back to the existing reactive download.
		return
	}

	delay := time.Duration(durationSeconds)*time.Second - prefetchLeadTime
	if delay < minPrefetchDelay {
		delay = minPrefetchDelay
	}

	timer := time.AfterFunc(delay, func() {
		c.prefetchNextTrack(bot, chatID)
	})

	c.prefetchMu.Lock()
	c.prefetchTimers[chatID] = timer
	c.prefetchMu.Unlock()
}

// cancelPrefetch stops and forgets any pending prefetch timer for chatID.
// Safe to call even if none is scheduled.
func (c *TelegramCalls) cancelPrefetch(chatID int64) {
	c.prefetchMu.Lock()
	timer, ok := c.prefetchTimers[chatID]
	if ok {
		delete(c.prefetchTimers, chatID)
	}
	c.prefetchMu.Unlock()

	if ok {
		timer.Stop()
	}
}

// prefetchNextTrack downloads whatever is currently second in the chat's
// queue, if it isn't already downloaded. It re-reads the queue at fire time
// rather than trusting anything captured when the timer was scheduled, so
// it's automatically correct even if the queue was reordered, had tracks
// removed, or the chat stopped playing entirely in the meantime.
func (c *TelegramCalls) prefetchNextTrack(bot *td.Client, chatID int64) {
	if !cache.ChatCache.IsActive(chatID) {
		return
	}

	next := cache.ChatCache.GetUpcomingTrack(chatID)
	if next == nil || next.FilePath != "" {
		return
	}

	if !prefetchGuard.tryStart(next) {
		return
	}
	defer prefetchGuard.finish(next)

	logger.Info("[Prefetch] downloading upcoming track in background", "chat_id", chatID, "name", next.Name)

	dlPath, err := dl.DownloadCachedTrack(next, bot)
	if err != nil || dlPath == "" {
		logger.Warn("[Prefetch] failed to prefetch upcoming track, will fall back to normal download",
			"chat_id", chatID, "name", next.Name, "error", err)
		return
	}

	next.FilePath = dlPath
	logger.Info("[Prefetch] upcoming track is ready", "chat_id", chatID, "name", next.Name)
}

// trackPrefetchGuard prevents the same *utils.CachedTrack from being
// downloaded by two goroutines at once - e.g. a scheduled prefetch still in
// flight when the user skips straight into that track and playSong's normal
// downloadAndPrepareSong path also starts a download for it.
type trackPrefetchGuard struct {
	mu       sync.Mutex
	inFlight map[*utils.CachedTrack]bool
}

func (g *trackPrefetchGuard) tryStart(t *utils.CachedTrack) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight[t] {
		return false
	}
	g.inFlight[t] = true
	return true
}

func (g *trackPrefetchGuard) finish(t *utils.CachedTrack) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.inFlight, t)
}

var prefetchGuard = &trackPrefetchGuard{inFlight: make(map[*utils.CachedTrack]bool)}
