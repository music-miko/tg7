/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"ashokshau/tgmusic/internal/db"
	"fmt"
	"strings"

	td "github.com/AshokShau/gotdbot"
)

// gsRow renders one <tr> of the group-stats table.
func gsRow(label string, count int64) string {
	return fmt.Sprintf("<tr><td align='left'>%s</td><td align='right'>%d</td></tr>", label, count)
}

// groupStatsHandler handles the /gs command: a developer-facing breakdown
// of how many groups the bot has been added to today, this week, this
// month, this year, and in total.
func groupStatsHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	stats, err := db.Instance.GetChatJoinStats()
	if err != nil {
		_, _ = m.ReplyText(c, fmt.Sprintf("Failed to fetch group stats: %v", err), nil)
		return td.EndGroups
	}

	var sb strings.Builder
	sb.WriteString("<h3>📊 Group Join Stats</h3>")

	sb.WriteString("<table bordered striped>")
	sb.WriteString("<tr><th>Period</th><th>Groups</th></tr>")
	sb.WriteString(gsRow("Today", stats.Today))
	sb.WriteString(gsRow("Last 7 days", stats.Last7Days))
	sb.WriteString(gsRow("Last 30 days", stats.Last30Days))
	sb.WriteString(gsRow("Last year", stats.LastYear))
	sb.WriteString(gsRow("Total", stats.Total))
	sb.WriteString("</table>")

	if stats.GroupsBeforeTracking > 0 {
		sb.WriteString(fmt.Sprintf(
			"<blockquote>ℹ️ <code>%d</code> of the total were added before join-date tracking was enabled, so they aren't broken down by period above.</blockquote>",
			stats.GroupsBeforeTracking,
		))
	}

	richMessage := &td.InputRichMessage{Source: &td.RichMessageSourceHtml{Text: sb.String()}}
	_, err = m.ReplyRichMessage(c, richMessage, nil)
	return err
}
