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
	"strings"

	td "github.com/AshokShau/gotdbot"
)

// This file wires up Telegram Bot API 10.1 "Rich Messages"
// (https://core.telegram.org/bots/api#rich-messages), which is a proper
// superset of the old parse_mode=HTML formatting: on top of <b>/<i>/<code>
// etc. it adds real block-level elements — <h1>-<h6> headings, <table>,
// <details>/<summary>, <blockquote expandable>, <tg-time>, dividers, and
// more — that plain parse_mode HTML silently can't render.
//
// Several handlers in this package (stats, yt, as, gs, queue, help, the
// setup guide) already wrote their output using that markup, but sent it
// through the legacy SendTextMessageOpts{ParseMode: "HTML"} /
// EditTextMessageOpts{ParseMode: "HTML"} path, where tags like <table> and
// <details> are not understood and are shown to the user as literal text.
// The helpers below route the exact same markup through
// InputRichMessage + sendRichMessage/editMessageText instead, so it
// actually renders.
//
// One hard constraint worth knowing: rich blocks (tables, details,
// headings, ...) can only live in a message's *text*, never in a media
// caption — Telegram has no "rich caption". That's why the private /start
// screen embeds its welcome image as an in-message <img> tag instead of
// being sent as a separate photo with a caption: keeping it as ordinary
// Rich Message text means every screen it can navigate to (help, setup
// guide, and back) is a plain in-place edit, with nothing ever deleted
// and resent.
//
// A second, easy-to-miss difference from parse_mode=HTML: plain "\n"
// characters in ordinary HTML messages render as line breaks, but Rich
// HTML follows real HTML whitespace rules and collapses raw newlines, so
// text built with "\n" (like every handler in this package does) comes out
// as one run-together line instead of the intended multi-line layout.
// injectLineBreaks below inserts an explicit <br> before each "\n" so the
// output keeps the same line breaks it had under parse_mode=HTML.

// injectLineBreaks inserts a <br> before every newline in htmlText, since
// Rich HTML (unlike legacy parse_mode=HTML) treats a bare "\n" as
// insignificant whitespace and collapses it instead of rendering a line
// break.
func injectLineBreaks(htmlText string) string {
	return strings.ReplaceAll(htmlText, "\n", "<br>\n")
}

// richHTML wraps Telegram Rich HTML markup into a sendable InputRichMessage.
// DetectAutomaticBlocks lets Telegram auto-linkify plain URLs, @mentions,
// #hashtags, and similar, the same way it does for ordinary messages.
func richHTML(htmlText string) *td.InputRichMessage {
	return &td.InputRichMessage{
		DetectAutomaticBlocks: true,
		Source:                td.RichMessageSourceHtml{Text: injectLineBreaks(htmlText)},
	}
}

// sendRich sends a brand-new rich message to chatId.
func sendRich(c *td.Client, chatId int64, htmlText string, markup td.ReplyMarkup) (*td.Message, error) {
	return c.SendRichMessage(chatId, richHTML(htmlText), &td.SendTextMessageOpts{
		DisableWebPagePreview: true,
		ReplyMarkup:           markup,
	})
}

// replyRich replies to m with a rich message.
func replyRich(c *td.Client, m *td.Message, htmlText string, markup td.ReplyMarkup) (*td.Message, error) {
	return m.ReplyRichMessage(c, richHTML(htmlText), &td.SendTextMessageOpts{
		DisableWebPagePreview: true,
		ReplyMarkup:           markup,
	})
}

// editRich replaces msg's own content with rich content in place. Only
// valid when msg is already a text/rich message.
func editRich(c *td.Client, msg *td.Message, htmlText string, markup td.ReplyMarkup) (*td.Message, error) {
	return msg.EditContent(c, &td.InputMessageRichMessage{Message: richHTML(htmlText)}, markup)
}

// editRichByID does the same as editRich but addresses the message by
// chat/message ID, for use from callback-query handlers that haven't
// already fetched the *td.Message.
func editRichByID(c *td.Client, chatId, messageId int64, htmlText string, markup td.ReplyMarkup) (*td.Message, error) {
	content := &td.InputMessageRichMessage{Message: richHTML(htmlText)}
	return c.EditMessageText(chatId, content, messageId, &td.EditMessageTextOpts{ReplyMarkup: markup})
}

// headingBlock renders a Rich HTML heading, clamped to the supported h1-h6 range.
func headingBlock(level int, text string) string {
	if level < 1 {
		level = 1
	} else if level > 6 {
		level = 6
	}
	return fmt.Sprintf("<h%d>%s</h%d>", level, text, level)
}

// dividerBlock renders a horizontal divider between sections.
func dividerBlock() string {
	return "<hr>"
}
