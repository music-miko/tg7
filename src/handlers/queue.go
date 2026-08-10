/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/utils"
	"fmt"
	"html"
	"math"
	"strings"

	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/vc"

	td "github.com/AshokShau/gotdbot"
)

// queueHandler displays the current playback queue with detailed information.
func queueHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId

	chat, err := c.GetChat(chatID)
	if err != nil {
		_, _ = m.ReplyText(c, "Error fetching chat information.", nil)
		return nil
	}

	queue := cache.ChatCache.GetQueue(chatID)
	if len(queue) == 0 {
		_, _ = m.ReplyText(c, "The queue is empty.", nil)
		return nil
	}

	if !cache.ChatCache.IsActive(chatID) {
		_, _ = m.ReplyText(c, "The bot is not streaming in the video chat.", nil)
		return nil
	}

	current := queue[0]
	playedTime, _ := vc.Calls.PlayedTime(chatID)

	var b strings.Builder
	b.WriteString(headingBlock(4, fmt.Sprintf("Queue for %s", html.EscapeString(chat.Title))))
	b.WriteString("\n\n")

	b.WriteString("<b>Now Playing:</b>\n")
	b.WriteString(fmt.Sprintf("• <b>Title:</b> <code>%s</code>\n", html.EscapeString(truncate(current.Name, 45))))
	b.WriteString(fmt.Sprintf("• <b>By:</b> %s\n", html.EscapeString(current.User)))
	b.WriteString(fmt.Sprintf("• <b>Duration:</b> %s min\n", utils.SecToMin(current.Duration)))
	b.WriteString("• <b>Loop:</b> ")
	if current.Loop > 0 {
		b.WriteString("On\n")
	} else {
		b.WriteString("Off\n")
	}
	b.WriteString("• <b>Progress:</b> ")
	if playedTime > 0 && playedTime < math.MaxInt {
		b.WriteString(utils.SecToMin(int(playedTime)))
	} else {
		b.WriteString("0:00")
	}
	b.WriteString(" min\n")

	if len(queue) > 1 {
		b.WriteString(fmt.Sprintf("\n<b>Next Up (%d):</b>\n", len(queue)-1))
		b.WriteString("<table bordered striped>")
		b.WriteString("<tr><th align=\"center\">#</th><th>Title</th><th align=\"center\">By</th><th align=\"center\">Duration</th></tr>")

		for i, song := range queue[1:] {
			if i >= 14 {
				break
			}
			b.WriteString(fmt.Sprintf(
				"<tr><td align=\"center\">%d</td><td align=\"left\">%s</td><td align=\"center\">%s</td><td align=\"center\">%s</td></tr>",
				i+1,
				html.EscapeString(truncate(song.Name, 35)),
				html.EscapeString(truncate(song.User, 20)),
				utils.SecToMin(song.Duration),
			))
		}
		b.WriteString("</table>\n")

		if len(queue) > 15 {
			b.WriteString(fmt.Sprintf("<i>...and %d more tracks</i>\n", len(queue)-15))
		}
	}

	b.WriteString(fmt.Sprintf("\n<b>Total:</b> %d tracks", len(queue)))

	// Rich messages allow up to 32,768 UTF-8 bytes (with a "Show more" button
	// appearing after roughly the first 8,000), a much higher ceiling than
	// the old 4,096-character plain-message limit this fallback used to
	// guard against — kept here as a sane cap for pathologically long queues.
	text := b.String()
	if len(text) > 8000 {
		var sb strings.Builder
		progress := "0:00"
		if playedTime > 0 && playedTime < math.MaxInt {
			progress = utils.SecToMin(int(playedTime))
		}
		sb.WriteString(fmt.Sprintf(
			"%s\n\n<b>Now Playing:</b>\n• <code>%s</code>\n• %s/%s min\n\n<b>Total:</b> %d tracks",
			headingBlock(4, fmt.Sprintf("Queue for %s", html.EscapeString(chat.Title))),
			html.EscapeString(truncate(current.Name, 45)),
			progress,
			utils.SecToMin(current.Duration),
			len(queue),
		))
		text = sb.String()
	}

	_, err = replyRich(c, m, text, nil)
	return err
}
