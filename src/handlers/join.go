/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/vc"
	"fmt"
	"html"
	"regexp"

	td "github.com/AshokShau/gotdbot"
)

var (
	inviteLinkRE = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?(?:t\.me|telegram\.me)/(?:\+[A-Za-z0-9_-]+|joinchat/[A-Za-z0-9_-]+|[A-Za-z0-9_]{5,32})`)
	usernameRE   = regexp.MustCompile(`@[A-Za-z0-9_]{4,32}`)
)

// extractInviteLink pulls a t.me invite link or an @username out of free text.
func extractInviteLink(input string) string {
	if link := inviteLinkRE.FindString(input); link != "" {
		return link
	}
	return usernameRE.FindString(input)
}

// joinHandler handles /join and /link: makes this chat's assistant join the
// group right now, optionally through an invite link supplied by the admin
// (useful when the bot can't create invite links itself).
func joinHandler(c *td.Client, m *td.Message) error {
	if m.IsPrivate() {
		_, _ = m.ReplyText(c, "This command can only be used in group chats.", nil)
		return td.EndGroups
	}

	// Devs first so adminMode's "you must be an admin" reply doesn't fire for them.
	if !isDev(c, m) && !adminMode(c, m) {
		return td.EndGroups
	}

	link := extractInviteLink(Args(m))
	reply, err := m.ReplyText(c, "Attempting to join the assistant...", nil)
	if err != nil {
		c.Logger.Warn("failed to send join message", "chat_id", m.ChatId, "error", err)
		return td.EndGroups
	}

	assistant, err := vc.Calls.JoinAssistant(c, m.ChatId, link)
	if err != nil {
		_, _ = reply.EditText(c, "Failed to join the assistant:\n\n"+html.EscapeString(err.Error()), &td.EditTextMessageOpts{ParseMode: "HTML"})
		return td.EndGroups
	}

	me := assistant.App.Me()
	_, _ = reply.EditText(c,
		fmt.Sprintf("Assistant (<b>%s</b> | ID: <code>%d</code>) is in the group.", html.EscapeString(me.FirstName), me.ID),
		&td.EditTextMessageOpts{ParseMode: "HTML"},
	)
	return td.EndGroups
}
