/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

// ntgcalls reports the position of the *current ffmpeg source*, and a seek
// restarts ffmpeg with "-ss N", so the reported time drops back to 0. To keep
// PlayedTime (used by /seek and /queue) correct across seeks, we remember the
// position the current source started at and add it back.

// setPlayedOffset records where in the track the current source started.
func (c *TelegramCalls) setPlayedOffset(chatID int64, offset uint64) {
	c.offsetMu.Lock()
	defer c.offsetMu.Unlock()
	c.timeOffsets[chatID] = offset
}

// playedOffset returns where in the track the current source started (0 if it
// started from the beginning).
func (c *TelegramCalls) playedOffset(chatID int64) uint64 {
	c.offsetMu.RLock()
	defer c.offsetMu.RUnlock()
	return c.timeOffsets[chatID]
}

// clearPlayedOffset forgets the offset; call it whenever a track starts from 0
// or the call ends.
func (c *TelegramCalls) clearPlayedOffset(chatID int64) {
	c.offsetMu.Lock()
	defer c.offsetMu.Unlock()
	delete(c.timeOffsets, chatID)
}
