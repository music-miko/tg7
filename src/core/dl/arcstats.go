/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"sync"
	"sync/atomic"
	"time"
)

// Arc download/search counters. Kept as package-level atomics so the hot
// download path (arcMusic.resolve) never has to take a lock.
var (
	arcAudioAttempts int64
	arcAudioSuccess  int64
	arcAudioFailed   int64

	arcVideoAttempts int64
	arcVideoSuccess  int64
	arcVideoFailed   int64

	arcCacheHits       int64 // resolved from the local/Telegram direct-DB cache, no network call
	arcAPISuccess      int64 // resolved via the real ArcMusic job pipeline (create -> poll -> save)
	arcAPIFailed       int64
	arcFallbackToYtDlp int64 // ArcMusic failed and yt-dlp had to take over
	arcSearchAttempts  int64
	arcSearchFailed    int64

	arcTotalResolveNs int64 // sum of successful API resolve durations, for average calc
)

var (
	arcStatsMu        sync.RWMutex
	arcLastFailureAt  time.Time
	arcLastFailureMsg string
	arcLastSuccessAt  time.Time
	arcStatsStarted   = time.Now()
)

// recordArcAttempt marks the start of a resolve() call for the given media kind.
func recordArcAttempt(video bool) {
	if video {
		atomic.AddInt64(&arcVideoAttempts, 1)
	} else {
		atomic.AddInt64(&arcAudioAttempts, 1)
	}
}

// recordArcSuccess marks a successful resolve(), distinguishing a local
// cache hit (no API call) from an actual ArcMusic API round-trip.
func recordArcSuccess(video, cacheHit bool, dur time.Duration) {
	if video {
		atomic.AddInt64(&arcVideoSuccess, 1)
	} else {
		atomic.AddInt64(&arcAudioSuccess, 1)
	}

	if cacheHit {
		atomic.AddInt64(&arcCacheHits, 1)
	} else {
		atomic.AddInt64(&arcAPISuccess, 1)
		atomic.AddInt64(&arcTotalResolveNs, dur.Nanoseconds())
	}

	arcStatsMu.Lock()
	arcLastSuccessAt = time.Now()
	arcStatsMu.Unlock()
}

// recordArcFailure marks a failed resolve() (the API pipeline was exhausted).
func recordArcFailure(video bool, err error) {
	if video {
		atomic.AddInt64(&arcVideoFailed, 1)
	} else {
		atomic.AddInt64(&arcAudioFailed, 1)
	}
	atomic.AddInt64(&arcAPIFailed, 1)

	arcStatsMu.Lock()
	arcLastFailureAt = time.Now()
	if err != nil {
		arcLastFailureMsg = err.Error()
	}
	arcStatsMu.Unlock()
}

// recordArcFallback marks that yt-dlp had to be used because ArcMusic failed.
func recordArcFallback() {
	atomic.AddInt64(&arcFallbackToYtDlp, 1)
}

// recordArcSearch marks an ArcMusic search attempt (used as a fallback when
// InnerTube search fails) and whether it failed.
func recordArcSearch(failed bool) {
	atomic.AddInt64(&arcSearchAttempts, 1)
	if failed {
		atomic.AddInt64(&arcSearchFailed, 1)
	}
}

// ArcStatsSnapshot is a read-only view of the current Arc API statistics,
// safe to render directly in a Telegram message.
type ArcStatsSnapshot struct {
	StartedAt time.Time

	AudioAttempts, AudioSuccess, AudioFailed int64
	VideoAttempts, VideoSuccess, VideoFailed int64

	CacheHits       int64
	APISuccess      int64
	APIFailed       int64
	FallbackToYtDlp int64

	SearchAttempts int64
	SearchFailed   int64

	AvgResolveTime time.Duration

	LastSuccessAt  time.Time
	LastFailureAt  time.Time
	LastFailureMsg string
}

// TotalAttempts returns the combined audio+video resolve attempts.
func (s ArcStatsSnapshot) TotalAttempts() int64 {
	return s.AudioAttempts + s.VideoAttempts
}

// TotalSuccess returns the combined audio+video resolve successes.
func (s ArcStatsSnapshot) TotalSuccess() int64 {
	return s.AudioSuccess + s.VideoSuccess
}

// TotalFailed returns the combined audio+video resolve failures.
func (s ArcStatsSnapshot) TotalFailed() int64 {
	return s.AudioFailed + s.VideoFailed
}

// SuccessRate returns the overall success percentage (0-100).
func (s ArcStatsSnapshot) SuccessRate() float64 {
	total := s.TotalAttempts()
	if total == 0 {
		return 0
	}
	return float64(s.TotalSuccess()) / float64(total) * 100
}

// AudioSuccessRate returns the audio-only success percentage (0-100).
func (s ArcStatsSnapshot) AudioSuccessRate() float64 {
	if s.AudioAttempts == 0 {
		return 0
	}
	return float64(s.AudioSuccess) / float64(s.AudioAttempts) * 100
}

// VideoSuccessRate returns the video-only success percentage (0-100).
func (s ArcStatsSnapshot) VideoSuccessRate() float64 {
	if s.VideoAttempts == 0 {
		return 0
	}
	return float64(s.VideoSuccess) / float64(s.VideoAttempts) * 100
}

// SearchSuccessRate returns the ArcMusic search success percentage (0-100).
func (s ArcStatsSnapshot) SearchSuccessRate() float64 {
	if s.SearchAttempts == 0 {
		return 0
	}
	return float64(s.SearchAttempts-s.SearchFailed) / float64(s.SearchAttempts) * 100
}

// GetArcStats returns a snapshot of the current ArcMusic API statistics.
func GetArcStats() ArcStatsSnapshot {
	arcStatsMu.RLock()
	defer arcStatsMu.RUnlock()

	apiSuccess := atomic.LoadInt64(&arcAPISuccess)
	var avg time.Duration
	if apiSuccess > 0 {
		avg = time.Duration(atomic.LoadInt64(&arcTotalResolveNs) / apiSuccess)
	}

	return ArcStatsSnapshot{
		StartedAt: arcStatsStarted,

		AudioAttempts: atomic.LoadInt64(&arcAudioAttempts),
		AudioSuccess:  atomic.LoadInt64(&arcAudioSuccess),
		AudioFailed:   atomic.LoadInt64(&arcAudioFailed),

		VideoAttempts: atomic.LoadInt64(&arcVideoAttempts),
		VideoSuccess:  atomic.LoadInt64(&arcVideoSuccess),
		VideoFailed:   atomic.LoadInt64(&arcVideoFailed),

		CacheHits:       atomic.LoadInt64(&arcCacheHits),
		APISuccess:      apiSuccess,
		APIFailed:       atomic.LoadInt64(&arcAPIFailed),
		FallbackToYtDlp: atomic.LoadInt64(&arcFallbackToYtDlp),

		SearchAttempts: atomic.LoadInt64(&arcSearchAttempts),
		SearchFailed:   atomic.LoadInt64(&arcSearchFailed),

		AvgResolveTime: avg,

		LastSuccessAt:  arcLastSuccessAt,
		LastFailureAt:  arcLastFailureAt,
		LastFailureMsg: arcLastFailureMsg,
	}
}

// ResetArcStats clears all counters and restarts the tracking window.
func ResetArcStats() {
	atomic.StoreInt64(&arcAudioAttempts, 0)
	atomic.StoreInt64(&arcAudioSuccess, 0)
	atomic.StoreInt64(&arcAudioFailed, 0)
	atomic.StoreInt64(&arcVideoAttempts, 0)
	atomic.StoreInt64(&arcVideoSuccess, 0)
	atomic.StoreInt64(&arcVideoFailed, 0)
	atomic.StoreInt64(&arcCacheHits, 0)
	atomic.StoreInt64(&arcAPISuccess, 0)
	atomic.StoreInt64(&arcAPIFailed, 0)
	atomic.StoreInt64(&arcFallbackToYtDlp, 0)
	atomic.StoreInt64(&arcSearchAttempts, 0)
	atomic.StoreInt64(&arcSearchFailed, 0)
	atomic.StoreInt64(&arcTotalResolveNs, 0)

	arcStatsMu.Lock()
	arcLastFailureAt = time.Time{}
	arcLastFailureMsg = ""
	arcLastSuccessAt = time.Time{}
	arcStatsStarted = time.Now()
	arcStatsMu.Unlock()
}
