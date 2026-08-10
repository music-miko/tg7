/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/core/cache"
	"fmt"

	td "github.com/AshokShau/gotdbot"
)

// autoplayHandler handles the /autoplay command. It shows the current
// autoplay state for the chat along with a toggle button, mirroring the
// pattern used by /settings.
func autoplayHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId

	if cache.ChatCache.GetPlayingTrack(chatID) == nil {
		_, err := m.ReplyText(c, "Bot is not streaming in the video chat.", nil)
		return err
	}

	state := cache.ChatCache.GetAutoplay(chatID)
	text := autoplayText()
	button := autoplayButton(state)

	_, err := replyRich(c, m, text, button)
	return err
}

// autoplayCallbackHandler handles the toggle button on the /autoplay panel.
func autoplayCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !adminModeCB(c, cb) {
		return nil
	}

	chatID := cb.ChatId
	if cache.ChatCache.GetPlayingTrack(chatID) == nil {
		_ = cb.Answer(c, 0, true, "Bot is not streaming in the video chat.", "")
		return nil
	}

	state := cache.ChatCache.GetAutoplay(chatID)
	newState := !state
	cache.ChatCache.SetAutoplay(chatID, newState)

	text := autoplayText()
	button := autoplayButton(newState)

	if _, err := editRichByID(c, cb.ChatId, cb.MessageId, text, button); err != nil {
		c.Logger.Warn("Failed to edit autoplay message", "error", err)
	}

	var status string
	if newState {
		status = "enabled"
	} else {
		status = "disabled"
	}
	_ = cb.Answer(c, 0, false, fmt.Sprintf("Autoplay has been %s.", status), "")

	return nil
}

// autoplayText is the body shown on the /autoplay panel.
func autoplayText() string {
	return headingBlock(3, "🔁 Autoplay Control") +
		"\nWhen autoplay is on, the bot picks a related track from YouTube and keeps playing automatically once the queue runs out — no need to queue anything up manually."
}

// autoplayButton renders the single toggle button for the /autoplay panel,
// reflecting the current state in its label.
func autoplayButton(state bool) *td.ReplyMarkupInlineKeyboard {
	text := "Autoplay: OFF | ❌"
	if state {
		text = "Autoplay: ON | ✅"
	}

	return &td.ReplyMarkupInlineKeyboard{
		Rows: [][]td.InlineKeyboardButton{
			{
				{
					Text: text,
					Type: &td.InlineKeyboardButtonTypeCallback{
						Data: []byte("autoplay_toggle"),
					},
				},
			},
		},
	}
}
