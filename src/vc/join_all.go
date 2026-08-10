/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"time"

	td "github.com/AshokShau/gotdbot"
)

// joinAllDelay is the pause between joining consecutive assistants into the
// same chat. Kept conservative for the same reason as leaveDelay: joins
// share the same per-chat invite link, so firing them back-to-back risks
// tripping Telegram's join-rate limits.
const joinAllDelay = 2 * time.Second

// AssistantJoinResult is the outcome of trying to join a single assistant
// into a chat via JoinAllAssistants.
type AssistantJoinResult struct {
	Index    int
	UserID   int64
	Username string
	Err      error
}

// Success reports whether this assistant ended up a member of the chat.
func (r AssistantJoinResult) Success() bool {
	return r.Err == nil
}

// JoinAllAssistants makes every currently running assistant join chatID
// (skipping any that are already a member), using the same invite-link
// join flow as normal per-chat assignment. It's meant for one-off admin
// actions like adding every assistant to the logger group, so it runs
// sequentially with a short delay between assistants rather than in
// parallel, to avoid flooding on the shared invite link.
func (c *TelegramCalls) JoinAllAssistants(bot *td.Client, chatID int64) []AssistantJoinResult {
	c.mu.RLock()
	type indexed struct {
		index int
		call  *Assistant
	}
	var ordered []indexed
	for index, call := range c.assistants {
		ordered = append(ordered, indexed{index, call})
	}
	c.mu.RUnlock()

	results := make([]AssistantJoinResult, 0, len(ordered))
	for i, entry := range ordered {
		me := entry.call.App.Me()
		err := c.joinAssistant(bot, chatID, entry.call, entry.index)
		results = append(results, AssistantJoinResult{
			Index:    entry.index,
			UserID:   me.ID,
			Username: me.Username,
			Err:      err,
		})

		if i < len(ordered)-1 {
			time.Sleep(joinAllDelay)
		}
	}

	return results
}
