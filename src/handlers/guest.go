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
	"html"
	"log/slog"

	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/core"

	td "github.com/AshokShau/gotdbot"
)

// guestQueryHandler answers Telegram's "Guest Bots" queries.
//
// Guest Bots let a bot be summoned into any chat — even one it never
// joined — by tagging its @username in a message or replying to one of its
// messages. Telegram doesn't deliver that as a normal message; it fires an
// UpdateNewGuestQuery instead, and the bot gets exactly one shot at a reply
// via AnswerGuestQuery, built the same way an inline-query result is built.
//
// Here that single reply mirrors the private /start card (StartImg photo +
// primary Add-to-Group button) but personalizes it with whoever summoned
// the bot, so it reads "<Bot> for <Name>" — the same "guest card" pattern
func guestQueryHandler(c *td.Client, u *td.UpdateNewGuestQuery) error {
	if u.Message == nil {
		return nil
	}

	name := firstName(c, u.Message)
	botName := c.Me.FirstName
	username := c.Me.Usernames.EditableUsername

	caption := fmt.Sprintf(
		"🎶 <b>%s for %s</b>\n\n"+
			"Thanks for the shout-out, %s! I'm a music bot for Telegram — stream from YouTube, Spotify, Apple Music, SoundCloud, Deezer, JioSaavn and more, right inside any group voice chat.\n\n"+
			"👇 Tap below to add me to your group.",
		html.EscapeString(botName), html.EscapeString(name), html.EscapeString(name),
	)

	formattedCaption, err := c.GetFormattedText(caption, nil, "HTML")
	if err != nil {
		return err
	}

	content := &td.InputMessagePhoto{
		Photo: &td.InputPhoto{
			Photo: td.InputFileRemote{Id: config.StartImg},
		},
		Caption: formattedCaption,
	}

	result := &td.InputInlineQueryResultPhoto{
		Id:                  "guest_start",
		Title:               fmt.Sprintf("%s for %s", botName, name),
		Description:         "Tap to add me to your group",
		PhotoUrl:            config.StartImg,
		ThumbnailUrl:        config.StartImg,
		PhotoWidth:          1280,
		PhotoHeight:         720,
		InputMessageContent: content,
		ReplyMarkup:         core.GuestReplyMarkup(username),
	}

	if _, err = c.AnswerGuestQuery(u.Id, result); err != nil {
		slog.Error("failed to answer guest query", "err", err)
		return err
	}

	return nil
}
