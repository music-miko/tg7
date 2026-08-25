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

// --- Ephemeral Messages (Bot API 10.2/10.3) --------------------------------
//
// Ephemeral messages are visible only to the bot and one specific chat
// member (see https://core.telegram.org/bots/features#ephemeral-messages),
// which makes them a good fit for dev-only output (shell command results,
// internal diagnostics, ...) that shouldn't sit around readable by anyone
// else in whatever chat the command was run from. A send becomes ephemeral
// simply by setting ReceiverUserID on the usual SendTextMessageOpts - no
// separate API. There's no documented support for swapping an ephemeral
// message's content for a *different* Rich Message via edit, so
// replyRichEphemeral below is send-only; callers that need to update an
// ephemeral status message (e.g. "Running..." -> final result) should send
// a second ephemeral message with the final content and deleteEphemeral
// the placeholder, rather than editing it in place.

// replyRichEphemeral replies to m with a rich message that only
// receiverUserId can see.
func replyRichEphemeral(c *td.Client, m *td.Message, receiverUserId int64, htmlText string, markup td.ReplyMarkup) (*td.Message, error) {
	return m.ReplyRichMessage(c, richHTML(htmlText), &td.SendTextMessageOpts{
		DisableWebPagePreview: true,
		ReplyMarkup:           markup,
		ReceiverUserID:        receiverUserId,
	})
}

// deleteEphemeral deletes an ephemeral message by its ephemeral ID, e.g. to
// clear a "Running..." placeholder once the final ephemeral result has
// been sent.
func deleteEphemeral(c *td.Client, chatId int64, ephemeralMessageId int32, receiverUserId int64) {
	if ephemeralMessageId == 0 {
		return
	}
	if err := c.DeleteEphemeralMessage(chatId, ephemeralMessageId, receiverUserId); err != nil {
		c.Logger.Warn("Failed to delete ephemeral placeholder", "error", err)
	}
}

// --- Documents in Rich Messages (Bot API 10.3) ------------------------------
//
// documentFromMessage extracts the *Document from a message, whether it's a
// plain document message or a Rich Message with a document block (see
// buttonRichMessageWithDocument in richtext.go). Any code that used to do a
// bare `m.Content.(*td.MessageDocument)` type assertion should go through
// this instead, since a document attached via the new Rich Message document
// block no longer has that content type.
func documentFromMessage(m *td.Message) *td.Document {
	if m == nil || m.Content == nil {
		return nil
	}

	switch content := m.Content.(type) {
	case *td.MessageDocument:
		return content.Document
	case *td.MessageRichMessage:
		if content.Message == nil {
			return nil
		}
		for _, block := range content.Message.Blocks {
			if doc, ok := block.(*td.PageBlockDocument); ok {
				return doc.Document
			}
		}
	}
	return nil
}

// downloadDocument downloads the file behind a *Document (as returned by
// documentFromMessage), resolving it first if it's a remote-only reference -
// the same remote-resolution step Message.Download does internally, which
// isn't available to us once the document has been pulled out of a Rich
// Message block by hand.
func downloadDocument(c *td.Client, doc *td.Document, priority int32, offset, limit int64, synchronous bool) (*td.File, error) {
	if doc == nil || doc.Document == nil {
		return nil, nil
	}

	f := doc.Document
	if f.Remote != nil {
		resolved, err := c.GetRemoteFile(f.Remote.Id, &td.GetRemoteFileOpts{})
		if err != nil {
			return nil, err
		}
		f = resolved
	}

	return f.Download(c, limit, offset, priority, &td.DownloadFileOpts{Synchronous: synchronous})
}

// --- Button Revolution (Bot API 10.3) -------------------------------------
//
// Bot API 10.3 (https://core.telegram.org/bots/api#richmessagebutton) lets
// buttons live *inside* a Rich Message's own content — as a native
// full-width or custom-aligned row, with real styles (default, primary,
// success, danger, link) — instead of only ever trailing the message as a
// separate reply_markup keyboard. TDLib exposes this the same way it's long
// exposed Instant View page layout: an inputPageBlockButtonRow block,
// carrying []InlineButton, that can sit anywhere in a rich message's block
// list. Since gotdbot's HTML rich-message path (richHTML above) hands
// Telegram a raw HTML string to parse server-side and HTML has no button
// syntax, a native button row can only be reached via the block-based
// RichMessageSourceBlocks source below — buttonRichMessage combines one
// plain-text paragraph with one native button row for exactly that case.
//
// This intentionally doesn't replace richHTML/editRich for the bigger
// screens (help menu, stats, setup guide, ...) — those lean on full HTML
// (tables, <details>, headings) that only the HTML source path renders, and
// still pair perfectly well with an ordinary trailing reply_markup keyboard.
// Native in-message buttons are for panels where the button *is* the point,
// like /autoplay's toggle.

// ButtonStyle names the native Rich Message button styles introduced by
// Bot API 10.3's Button Revolution.
type ButtonStyle int

const (
	ButtonStyleDefault ButtonStyle = iota
	ButtonStylePrimary
	ButtonStyleSuccess
	ButtonStyleDanger
	ButtonStyleLink
)

func (s ButtonStyle) toTd() td.ButtonStyle {
	switch s {
	case ButtonStylePrimary:
		return td.ButtonStylePrimary{}
	case ButtonStyleSuccess:
		return td.ButtonStyleSuccess{}
	case ButtonStyleDanger:
		return td.ButtonStyleDanger{}
	case ButtonStyleLink:
		return td.ButtonStyleLink{}
	default:
		return td.ButtonStyleDefault{}
	}
}

// RichButton describes one native in-message button for buttonRow.
type RichButton struct {
	Text  string
	Style ButtonStyle
	// Data is the callback data for a callback-style button. Leave empty
	// and set Url instead for a link-style button.
	Data string
	Url  string
}

func (b RichButton) toInline() td.InlineButton {
	var buttonType td.InlineKeyboardButtonType
	if b.Url != "" {
		buttonType = &td.InlineKeyboardButtonTypeUrl{Url: b.Url}
	} else {
		buttonType = &td.InlineKeyboardButtonTypeCallback{Data: []byte(b.Data)}
	}

	return td.InlineButton{
		Style: b.Style.toTd(),
		Text:  td.RichTextPlain{Text: b.Text},
		Type:  buttonType,
	}
}

// buttonRow renders buttons as a single native, full-width Rich Message
// button row block (Bot API 10.3 Button Revolution).
func buttonRow(buttons ...RichButton) td.InputPageBlock {
	inline := make([]td.InlineButton, 0, len(buttons))
	for _, b := range buttons {
		inline = append(inline, b.toInline())
	}
	return td.InputPageBlockButtonRow{Buttons: inline}
}

// buttonRichMessageSized builds an InputRichMessage out of a plain-text
// section heading (rendered at headingSize, 1 = largest / 6 = smallest), a
// plain-text paragraph body, and a native button row - the block-based
// counterpart to richHTML, used when the buttons need to live inside the
// message itself rather than in a trailing reply_markup. heading/body are
// sent as plain RichText (no HTML parsing happens on the blocks path - see
// the "Button Revolution" note above), so callers should pass plain text
// here, not markup built for richHTML.
func buttonRichMessageSized(heading, body string, headingSize int32, buttons ...RichButton) *td.InputRichMessage {
	blocks := make([]td.InputPageBlock, 0, 3)
	if heading != "" {
		blocks = append(blocks, td.InputPageBlockSectionHeading{
			Size: headingSize,
			Text: td.RichTextPlain{Text: heading},
		})
	}
	if body != "" {
		blocks = append(blocks, td.InputPageBlockParagraph{Text: td.RichTextPlain{Text: body}})
	}
	blocks = append(blocks, buttonRow(buttons...))

	return &td.InputRichMessage{
		DetectAutomaticBlocks: true,
		Source:                td.RichMessageSourceBlocks{Blocks: blocks},
	}
}

// buttonRichMessage is buttonRichMessageSized with the standard heading
// size (3) used by the toggle-panel screens (/autoplay, /mute, /pause).
func buttonRichMessage(heading, body string, buttons ...RichButton) *td.InputRichMessage {
	return buttonRichMessageSized(heading, body, 3, buttons...)
}

// sendButtonRich sends a new rich message with native in-message buttons
// (no trailing reply_markup - the buttons are already part of the content).
func sendButtonRich(c *td.Client, chatId int64, heading, body string, buttons ...RichButton) (*td.Message, error) {
	return c.SendRichMessage(chatId, buttonRichMessage(heading, body, buttons...), &td.SendTextMessageOpts{
		DisableWebPagePreview: true,
	})
}

// replyButtonRich replies to m with a rich message carrying native
// in-message buttons (no trailing reply_markup).
func replyButtonRich(c *td.Client, m *td.Message, heading, body string, buttons ...RichButton) (*td.Message, error) {
	return m.ReplyRichMessage(c, buttonRichMessage(heading, body, buttons...), nil)
}

// replyButtonRichSized is replyButtonRich with an explicit heading size,
// for screens (like the /play empty-query notice) that want a smaller,
// less shouty heading than the default toggle-panel size.
func replyButtonRichSized(c *td.Client, m *td.Message, heading, body string, headingSize int32, buttons ...RichButton) (*td.Message, error) {
	return m.ReplyRichMessage(c, buttonRichMessageSized(heading, body, headingSize, buttons...), nil)
}

// editButtonRichByID replaces a message's content in place with plain
// heading/body text plus a native button row, addressed by chat/message ID.
func editButtonRichByID(c *td.Client, chatId, messageId int64, heading, body string, buttons ...RichButton) (*td.Message, error) {
	content := &td.InputMessageRichMessage{Message: buttonRichMessage(heading, body, buttons...)}
	return c.EditMessageText(chatId, content, messageId, &td.EditMessageTextOpts{})
}

// documentBlock renders a local file path as a native Rich Message document
// block (Bot API 10.3's "documents attached to rich messages" - previously
// a file could only ever be its own separate message, never live alongside
// other rich content in the same one). captionText, if non-empty, is shown
// as a plain-text caption under the file - see the blocks-vs-HTML note
// above for why this stays plain text rather than richHTML markup.
func documentBlock(localPath, captionText string) td.InputPageBlock {
	block := td.InputPageBlockDocument{
		Document: &td.InputDocument{
			Document: td.InputFileLocal{Path: localPath},
		},
	}
	if captionText != "" {
		block.Caption = &td.PageBlockCaption{Text: td.RichTextPlain{Text: captionText}}
	}
	return block
}

// buttonRichMessageWithDocument is buttonRichMessage plus one attached
// local file, rendered as a native document block ahead of the heading -
// e.g. for pairing a generated report/backup file with an explanatory
// blurb and action buttons in a single message.
func buttonRichMessageWithDocument(localPath, docCaption, heading, body string, buttons ...RichButton) *td.InputRichMessage {
	msg := buttonRichMessage(heading, body, buttons...)
	blocks, ok := msg.Source.(td.RichMessageSourceBlocks)
	if !ok {
		return msg
	}
	blocks.Blocks = append([]td.InputPageBlock{documentBlock(localPath, docCaption)}, blocks.Blocks...)
	msg.Source = blocks
	return msg
}
