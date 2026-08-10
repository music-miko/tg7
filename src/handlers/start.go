/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/config"
	"fmt"
	"html"
	"runtime"
	"strings"
	"time"

	"ashokshau/tgmusic/src/core"
	"ashokshau/tgmusic/src/core/db"

	td "github.com/AshokShau/gotdbot"
)

// pingHandler handles the /ping command.
func pingHandler(c *td.Client, m *td.Message) error {

	start := time.Now()

	msg, err := m.ReplyText(c, "Pinging… please wait…", nil)
	if err != nil {
		return err
	}

	latency := time.Since(start).Milliseconds()
	uptime := getFormattedDuration(time.Since(startTime))

	response := fmt.Sprintf(
		"<b>📊 System Performance Metrics</b>\n\n"+
			"<b>Bot Latency:</b> <code>%d ms</code>\n"+
			"<b>Uptime:</b> <code>%s</code>\n"+
			"<b>Go Routines:</b> <code>%d</code>\n"+
			"<b>Version:</b> <code>%s</code>\n",
		latency, uptime, runtime.NumGoroutine(), config.Version,
	)

	_, err = msg.EditText(c, response, &td.EditTextMessageOpts{ParseMode: "HTML"})
	return err
}

// privateWelcomeText builds the private-chat /start body as Rich HTML,
// with the welcome image embedded directly via <img> instead of being sent
// as a separate photo message. Keeping the whole screen — image included —
// as one Rich Message means every other private-chat screen (help, setup
// guide) is just another Rich Message, so navigating between them is
// always a plain in-place edit; there's no photo/caption message to delete
// and recreate along the way.
func privateWelcomeText(name, botName string) string {
	escName := html.EscapeString(name)
	escBotName := html.EscapeString(botName)

	return fmt.Sprintf(
		"<img src=\"%s\"/>"+
			"%s"+
			"<p><b>%s</b> streams high-quality audio and video straight into your group's voice chat — from YouTube, Spotify, Apple Music, SoundCloud, Deezer, JioSaavn, and more.</p>"+
			"<p><b>Supported platforms:</b> YouTube, Spotify, Apple Music, SoundCloud, Deezer, JioSaavn and more.</p>"+
			"<p>Use the buttons below to add %s to your group, or explore everything it can do.</p>",
		config.StartImg,
		headingBlock(3, fmt.Sprintf("Welcome, %s! 👋", escName)),
		escBotName, escBotName,
	)
}

// groupWelcomeText builds the group-chat /start body as Rich HTML.
func groupWelcomeText(botName, uptime string) string {
	escBotName := html.EscapeString(botName)

	return fmt.Sprintf(
		"%s"+
			"<p><b>Uptime:</b> <code>%s</code></p>"+
			"<p><i>A feature-rich music bot for your group's voice chats — play, queue, and keep the music going with autoplay.</i></p>",
		headingBlock(3, fmt.Sprintf("%s is ready! 🎶", escBotName)),
		uptime,
	)
}

// startHandler handles the /start command.
func startHandler(c *td.Client, m *td.Message) error {
	chatID := m.ChatId

	if m.IsPrivate() {
		go func(chatID int64) {
			_ = db.Instance.AddUser(chatID)
		}(chatID)

		text := privateWelcomeText(firstName(c, m), c.Me.FirstName)
		_, err := replyRich(c, m, text, core.PrivateStartMarkup(c.Me.Usernames.EditableUsername))
		return err
	}

	go func(chatID int64) {
		_ = db.Instance.AddChat(chatID)
	}(chatID)

	uptime := getFormattedDuration(time.Since(startTime))
	text := groupWelcomeText(c.Me.FirstName, uptime)
	_, err := replyRich(c, m, text, core.GroupWelcomeMarkup())
	return err
}


func setupGuideText(botName string) string {
	escBotName := html.EscapeString(botName)

	stepper := "<table bordered striped>" +
		"<tr><th align=\"center\">Step</th><th>Action</th></tr>" +
		"<tr><td align=\"center\">1️⃣</td><td>Tap <b>➕ Add to Group</b> below and pick your group</td></tr>" +
		"<tr><td align=\"center\">2️⃣</td><td>Promote the bot to admin — tap <b>🔐 Admin rights</b> below for the exact list</td></tr>" +
		"<tr><td align=\"center\">3️⃣</td><td>Start a voice or video chat in that group</td></tr>" +
		"<tr><td align=\"center\">4️⃣</td><td>Run <code>/play song name</code> and enjoy 🎶</td></tr>" +
		"</table>"

	adminRights := detailsBlock("🔐 Admin rights", ""+
		"• <b>Invite Users via Link</b> — lets the bot's assistant account join your group's voice chat\n"+
		"• <b>Delete Messages</b> — lets the bot clean up its own command and status messages\n"+
		"• <b>Ban Users</b> — lets the bot auto-recover its assistant if it's ever muted or banned by mistake",
	)

	faq := detailsBlock("❓ Common questions", ""+
		"<b>Why does it need its own account in the voice chat?</b>\n"+
		"Telegram bots can't join voice chats directly — a regular \"assistant\" account streams the audio on the bot's behalf. That's what the admin rights above are for.\n\n"+
		"<b>I ran /play and nothing happened — why?</b>\n"+
		"Make sure a voice or video chat is actually live in the group first; the bot joins it, it doesn't start one.\n\n"+
		"<b>Can it keep playing after my queue ends?</b>\n"+
		"Yes — turn on <code>/autoplay</code> and it'll pick related tracks automatically once the queue runs dry.",
	)

	commandTable := "<table bordered striped>" +
		"<tr><th>Command</th><th>What it does</th></tr>" +
		"<tr><td align=\"left\"><code>/play [song]</code></td><td align=\"left\">Streams audio in the voice chat</td></tr>" +
		"<tr><td align=\"left\"><code>/vplay [song]</code></td><td align=\"left\">Streams video instead of audio</td></tr>" +
		"<tr><td align=\"left\"><code>/fplay [song]</code></td><td align=\"left\">Cuts a track to the front of the queue (admins)</td></tr>" +
		"<tr><td align=\"left\"><code>/autoplay</code></td><td align=\"left\">Keeps the music going once the queue is empty</td></tr>" +
		"<tr><td align=\"left\"><code>/queue</code></td><td align=\"left\">Shows what's playing and what's next</td></tr>" +
		"</table>"

	return fmt.Sprintf(
		"%s\n"+
			"<i>Get %s streaming in your group in under a minute.</i>\n\n"+
			"%s"+
			"%s"+
			"%s"+
			"%s\n\n"+
			"%s\n\n"+
			"<b>🎉 That's it — you're all set. Enjoy the music!</b>",
		headingBlock(2, "🅰️ Setup Guide"),
		escBotName,
		stepper,
		adminRights,
		dividerBlock(),
		faq,
		commandTable,
	)
}

// setupCallbackHandler handles the Setup Guide button and its Back
// navigation. Since /start, /help, and the setup guide are all Rich
// Messages now (the private welcome image lives in-message via <img>
// rather than as a separate photo), every transition here is a plain
// in-place edit — nothing is ever deleted and resent.
func setupCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	data := cb.DataString()

	switch {
	case strings.Contains(data, "setup_guide"):
		_ = cb.Answer(c, 0, false, "Opening setup guide...", "")
		text := setupGuideText(c.Me.FirstName)
		markup := core.GuideBackMarkup(c.Me.Usernames.EditableUsername)
		_, err := editRichByID(c, cb.ChatId, cb.MessageId, text, markup)
		return err

	case strings.Contains(data, "setup_back"):
		_ = cb.Answer(c, 0, false, "Returning...", "")

		if cb.IsPrivate() {
			user, err := c.GetUser(cb.SenderUserId)
			name := "there"
			if err == nil && user != nil {
				name = user.FirstName
			}
			text := privateWelcomeText(name, c.Me.FirstName)
			_, err = editRichByID(c, cb.ChatId, cb.MessageId, text, core.PrivateStartMarkup(c.Me.Usernames.EditableUsername))
			return err
		}

		uptime := getFormattedDuration(time.Since(startTime))
		text := groupWelcomeText(c.Me.FirstName, uptime)
		_, err := editRichByID(c, cb.ChatId, cb.MessageId, text, core.GroupWelcomeMarkup())
		return err
	}

	return nil
}
