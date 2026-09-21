/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"fmt"

	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/vc"

	td "github.com/AshokShau/gotdbot"
)

func pauseHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId

	if !cache.ChatCache.IsActive(chatID) {
		_, _ = m.ReplyText(c, "There is no active playback in the video chat.", nil)
		return nil
	}

	if _, err := vc.Calls.Pause(chatID); err != nil {
		_, _ = m.ReplyText(c, fmt.Sprintf("Failed to pause the playback: %s", err.Error()), nil)
		return nil
	}

	cache.ChatCache.SetPaused(chatID, true)
	_, err := replyButtonRich(c, m, "⏯ Pause Control", pauseText(true), pauseButton(true))
	return err
}

func resumeHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId

	if chatID > 0 {
		_, _ = m.ReplyText(c, "This command can only be used in a supergroup.", nil)
		return nil
	}

	if !cache.ChatCache.IsActive(chatID) {
		_, _ = m.ReplyText(c, "There is no active playback in the video chat.", nil)
		return nil
	}

	if _, err := vc.Calls.Resume(chatID); err != nil {
		_, _ = m.ReplyText(c, fmt.Sprintf("Failed to resume the playback: %s", err.Error()), nil)
		return nil
	}

	cache.ChatCache.SetPaused(chatID, false)
	_, err := replyButtonRich(c, m, "⏯ Pause Control", pauseText(false), pauseButton(false))
	return err
}

// pauseCallbackHandler handles the toggle button on the /pause and /resume
// panels (Bot API 10.3 Button Revolution - see richtext.go), mirroring the
// pattern used by /autoplay.
func pauseCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !adminModeCB(c, cb) {
		return nil
	}

	chatID := cb.ChatId
	if !cache.ChatCache.IsActive(chatID) {
		_ = cb.Answer(c, 0, true, "There is no active playback in the video chat.", "")
		return nil
	}

	paused := cache.ChatCache.GetPaused(chatID)
	newState := !paused

	var err error
	if newState {
		_, err = vc.Calls.Pause(chatID)
	} else {
		_, err = vc.Calls.Resume(chatID)
	}
	if err != nil {
		_ = cb.Answer(c, 0, true, fmt.Sprintf("Failed: %s", err.Error()), "")
		return nil
	}

	cache.ChatCache.SetPaused(chatID, newState)

	if _, err := editButtonRichByID(c, cb.ChatId, cb.MessageId, "⏯ Pause Control", pauseText(newState), pauseButton(newState)); err != nil {
		c.Logger.Warn("Failed to edit pause message", "error", err)
	}

	var status string
	if newState {
		status = "paused"
	} else {
		status = "resumed"
	}
	_ = cb.Answer(c, 0, false, fmt.Sprintf("Playback has been %s.", status), "")

	return nil
}

// pauseText is the plain-text body shown on the pause/resume panel, below
// the heading passed separately to buttonRichMessage.
func pauseText(paused bool) string {
	if paused {
		return "Playback is currently paused. Tap below to resume it."
	}
	return "Playback is currently playing. Tap below to pause it."
}

// pauseButton renders the single toggle button for the pause/resume panel
// as a native Rich Message button (Bot API 10.3 Button Revolution), styled
// danger/success to match its current state.
func pauseButton(paused bool) RichButton {
	if paused {
		return RichButton{Text: "⏸ Paused", Style: ButtonStyleDanger, Data: "playback_pause_toggle"}
	}
	return RichButton{Text: "▶ Playing", Style: ButtonStyleSuccess, Data: "playback_pause_toggle"}
}
