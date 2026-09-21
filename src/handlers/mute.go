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

func muteHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	if args := Args(m); args != "" {
		return td.EndGroups
	}

	chatID := m.ChatId
	if !cache.ChatCache.IsActive(chatID) {
		_, err := m.ReplyText(c, "There is no active playback in the video chat.", nil)
		return err
	}

	if _, err := vc.Calls.Mute(chatID); err != nil {
		_, err = m.ReplyText(c, fmt.Sprintf("Failed to mute the playback: %s", err.Error()), nil)
		return err
	}

	cache.ChatCache.SetMuted(chatID, true)
	_, err := replyButtonRich(c, m, "🔇 Mute Control", muteText(true), muteButton(true))
	return err
}

func unmuteHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	if args := Args(m); args != "" {
		return td.EndGroups
	}

	chatID := m.ChatId
	if !cache.ChatCache.IsActive(chatID) {
		_, err := m.ReplyText(c, "There is no active playback in the video chat.", nil)
		return err
	}

	if _, err := vc.Calls.Unmute(chatID); err != nil {
		_, err = m.ReplyText(c, fmt.Sprintf("Failed to unmute the playback: %s", err.Error()), nil)
		return err
	}

	cache.ChatCache.SetMuted(chatID, false)
	_, err := replyButtonRich(c, m, "🔇 Mute Control", muteText(false), muteButton(false))
	return err
}

// muteCallbackHandler handles the toggle button on the /mute and /unmute
// panels (Bot API 10.3 Button Revolution - see richtext.go), mirroring the
// pattern used by /autoplay.
func muteCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !adminModeCB(c, cb) {
		return nil
	}

	chatID := cb.ChatId
	if !cache.ChatCache.IsActive(chatID) {
		_ = cb.Answer(c, 0, true, "There is no active playback in the video chat.", "")
		return nil
	}

	muted := cache.ChatCache.GetMuted(chatID)
	newState := !muted

	var err error
	if newState {
		_, err = vc.Calls.Mute(chatID)
	} else {
		_, err = vc.Calls.Unmute(chatID)
	}
	if err != nil {
		_ = cb.Answer(c, 0, true, fmt.Sprintf("Failed: %s", err.Error()), "")
		return nil
	}

	cache.ChatCache.SetMuted(chatID, newState)

	if _, err := editButtonRichByID(c, cb.ChatId, cb.MessageId, "🔇 Mute Control", muteText(newState), muteButton(newState)); err != nil {
		c.Logger.Warn("Failed to edit mute message", "error", err)
	}

	var status string
	if newState {
		status = "muted"
	} else {
		status = "unmuted"
	}
	_ = cb.Answer(c, 0, false, fmt.Sprintf("Playback has been %s.", status), "")

	return nil
}

// muteText is the plain-text body shown on the mute/unmute panel, below
// the heading passed separately to buttonRichMessage.
func muteText(muted bool) string {
	if muted {
		return "Playback audio is currently muted. Tap below to unmute it."
	}
	return "Playback audio is currently audible. Tap below to mute it."
}

// muteButton renders the single toggle button for the mute/unmute panel as
// a native Rich Message button (Bot API 10.3 Button Revolution), styled
// danger/success to match its current state.
func muteButton(muted bool) RichButton {
	if muted {
		return RichButton{Text: "🔇 Muted", Style: ButtonStyleDanger, Data: "playback_mute_toggle"}
	}
	return RichButton{Text: "🔊 Unmuted", Style: ButtonStyleSuccess, Data: "playback_mute_toggle"}
}
