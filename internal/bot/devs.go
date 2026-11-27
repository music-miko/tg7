/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"ashokshau/tgmusic/internal/cache"
	"ashokshau/tgmusic/internal/calls"
	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/db"
	"ashokshau/tgmusic/internal/utils"
	"fmt"
	"html"
	"strings"

	td "github.com/AshokShau/gotdbot"
)

func activeVcHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	activeChats := cache.ChatCache.GetActiveChats()

	var sb strings.Builder
	sb.WriteString("<h3>🎵 Active Voice Chats</h3>")

	if len(activeChats) == 0 {
		sb.WriteString("<blockquote><b>🔇 No Active Chats:</b> There are currently no active voice or video chats.</blockquote>")
	} else {
		sb.WriteString(fmt.Sprintf("<p>There are currently <b>%d</b> active voice/video chat(s) running.</p>", len(activeChats)))
		sb.WriteString("<details>")
		sb.WriteString("<summary><b>📊 Click to Show Active Chats</b></summary>")
		sb.WriteString("<br>")
		sb.WriteString("<table bordered striped>")
		sb.WriteString("<tr>")
		sb.WriteString("<th align='center'><b>#</b></th>")
		sb.WriteString("<th align='center'><b>Chat ID</b></th>")
		sb.WriteString("<th align='center'><b>Queue</b></th>")
		sb.WriteString("<th align='left'><b>Now Playing Track Info</b></th>")
		sb.WriteString("</tr>")

		for i, chatID := range activeChats {
			queueLength := cache.ChatCache.GetQueueLength(chatID)
			currentSong := cache.ChatCache.GetPlayingTrack(chatID)

			var trackLink string
			if currentSong != nil {
				trackName := html.EscapeString(currentSong.Name)
				trackURL := html.EscapeString(currentSong.URL)
				if trackURL == "" {
					trackURL = config.SupportChannel
				}
				durStr := utils.SecToMin(currentSong.Duration)
				trackLink = fmt.Sprintf("<a href='%s'>%s</a> (%s)", trackURL, trackName, durStr)
			} else {
				trackLink = "<i>🔇 No song playing.</i>"
			}

			sb.WriteString("<tr>")
			sb.WriteString(fmt.Sprintf("<td align='center'>%d</td>", i+1))
			sb.WriteString(fmt.Sprintf("<td align='center'><code>%d</code></td>", chatID))
			sb.WriteString(fmt.Sprintf("<td align='center'>%d</td>", queueLength))
			sb.WriteString(fmt.Sprintf("<td align='left'>%s</td>", trackLink))
			sb.WriteString("</tr>")
		}
		sb.WriteString("</table>")
		sb.WriteString("</details>")
		sb.WriteString("<br>")
	}

	richMessage := &td.InputRichMessage{Source: &td.RichMessageSourceHtml{Text: sb.String()}}
	_, err := m.ReplyRichMessage(c, richMessage, nil)
	return err
}

// Handles the /clearass command to remove all assistant assignments
func clearAssistantsHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	done, err := db.Instance.ClearAllAssistants()
	if err != nil {
		c.Logger.Errorf("clear assistant assignments: %v", err)
		if _, replyErr := m.ReplyText(c, "Unable to clear assistant assignments. Please try again.", nil); replyErr != nil {
			c.Logger.Warnf("send clear assistants failure in chat %d: %v", m.ChatId, replyErr)
		}
		return td.EndGroups
	}

	_, err = m.ReplyText(c, fmt.Sprintf("Removed assistant from %d chats", done), nil)
	return err
}

func leaveAllHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	reply, err := m.ReplyText(c, "Assistant is leaving all chats...", nil)
	if err != nil {
		return err
	}

	leftCount, err := calls.Calls.LeaveAll()
	if err != nil {
		c.Logger.Errorf("leave all assistant chats: %v", err)
		if _, editErr := reply.EditText(c, "Unable to leave all chats. Please try again.", nil); editErr != nil {
			return fmt.Errorf("edit leave-all error in chat %d: %w", m.ChatId, editErr)
		}
		return fmt.Errorf("leave all assistant chats: %w", err)
	}

	_, err = reply.EditText(c, fmt.Sprintf("Assistant's Left %d chats", leftCount), nil)
	if err != nil {
		return fmt.Errorf("edit leave-all result in chat %d: %w", m.ChatId, err)
	}
	return nil
}

// asHandler handles the /as command: invites every running assistant into
// the configured logger group, and reports which ones joined vs failed.
func asHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if config.LoggerId == 0 {
		_, _ = m.ReplyText(c, "Please set LOGGER_ID in .env first.", nil)
		return td.EndGroups
	}

	results := calls.Calls.JoinAllAssistants(c, config.LoggerId)
	if len(results) == 0 {
		_, err := m.ReplyText(c, "No assistants are currently running.", nil)
		return err
	}

	var sb strings.Builder
	sb.WriteString("<h3>🤝 Assistant Invite Results</h3>")

	sb.WriteString("<table bordered striped>")
	sb.WriteString("<tr><th>Client</th><th>Assistant</th><th>Status</th></tr>")

	var joined, failed int
	var failLines []string
	for _, r := range results {
		name := fmt.Sprintf("%d", r.UserID)
		if r.Username != "" {
			name = "@" + r.Username
		}

		status := "✅ OK"
		if !r.Success() {
			failed++
			status = "❌ FAILED"
			failLines = append(failLines, fmt.Sprintf(
				"client%d (%s): %s", r.Index, html.EscapeString(name), html.EscapeString(truncate(r.Err.Error(), 150)),
			))
		} else {
			joined++
		}

		sb.WriteString(fmt.Sprintf(
			"<tr><td align='left'>client%d</td><td align='left'>%s</td><td align='left'>%s</td></tr>",
			r.Index, html.EscapeString(name), status,
		))
	}

	sb.WriteString(fmt.Sprintf(
		"<tr><td align='left'><b>Total</b></td><td></td><td align='left'><b>%d/%d</b></td></tr>",
		joined, joined+failed,
	))
	sb.WriteString("</table>")

	if len(failLines) > 0 {
		sb.WriteString("<details>")
		sb.WriteString("<summary><b>Failure details</b></summary>")
		sb.WriteString("<br>")
		sb.WriteString(strings.Join(failLines, "<br>"))
		sb.WriteString("</details>")
	}

	richMessage := &td.InputRichMessage{Source: &td.RichMessageSourceHtml{Text: sb.String()}}
	_, err := m.ReplyRichMessage(c, richMessage, nil)
	return err
}

func loggerHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if config.LoggerId == 0 {
		_, _ = m.ReplyText(c, "Please set LOGGER_ID in .env first.", nil)
		return td.EndGroups
	}

	args := strings.ToLower(Args(m))
	if len(args) == 0 {
		_, _ = m.ReplyText(c, fmt.Sprintf("Usage: /logger [enable|disable|on|off]\nCurrent status: %t", db.Instance.GetLoggerStatus()), nil)
		return td.EndGroups
	}

	var enabled bool
	switch args {
	case "enable", "on":
		enabled = true
	case "disable", "off":
		enabled = false
	default:
		_, _ = m.ReplyText(c, "Invalid argument. Use 'enable', 'disable', 'on', or 'off'.", nil)
		return td.EndGroups
	}
	if err := db.Instance.SetLoggerStatus(enabled); err != nil {
		c.Logger.Errorf("update logger status: %v", err)
		_, replyErr := m.ReplyText(c, "Unable to update logger status. Please try again.", nil)
		if replyErr != nil {
			return replyErr
		}
		return td.EndGroups
	}
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	if _, err := m.ReplyText(c, "Logger "+status, nil); err != nil {
		return err
	}

	return td.EndGroups
}
