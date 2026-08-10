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
	"time"

	"ashokshau/tgmusic/src/core/dl"

	td "github.com/AshokShau/gotdbot"
)

// arcTableRow renders one <tr> of the Arc stats table.
func arcTableRow(label string, attempts, ok, fail int64, rate float64) string {
	return fmt.Sprintf(
		"<tr><td align=\"left\">%s</td><td align=\"right\">%d</td><td align=\"right\">%d</td><td align=\"right\">%d</td><td align=\"right\">%.1f%%</td></tr>",
		label, attempts, ok, fail, rate,
	)
}

// ytStatsHandler handles the /yt command: a developer-facing dashboard of
// ArcMusic API download/search success rates, rendered as a real HTML table
// (see https://core.telegram.org/bots/api#rich-html-style) plus an
// expandable blockquote for the finer-grained details.
func ytStatsHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	args := strings.Fields(Args(m))
	if len(args) > 0 && strings.EqualFold(args[0], "reset") {
		dl.ResetArcStats()
		_, err := m.ReplyText(c, "✅ ArcMusic API stats have been reset.", nil)
		return err
	}

	stats := dl.GetArcStats()

	var sb strings.Builder
	sb.WriteString(headingBlock(2, "🎧 ArcMusic API Stats"))
	sb.WriteString("\n\n")

	sb.WriteString("<table bordered striped>")
	sb.WriteString("<tr><th>Kind</th><th>Total</th><th>OK</th><th>Fail</th><th>Rate</th></tr>")
	sb.WriteString(arcTableRow("Audio", stats.AudioAttempts, stats.AudioSuccess, stats.AudioFailed, stats.AudioSuccessRate()))
	sb.WriteString(arcTableRow("Video", stats.VideoAttempts, stats.VideoSuccess, stats.VideoFailed, stats.VideoSuccessRate()))
	sb.WriteString(arcTableRow("Overall", stats.TotalAttempts(), stats.TotalSuccess(), stats.TotalFailed(), stats.SuccessRate()))
	sb.WriteString("</table>\n\n")

	sb.WriteString("<blockquote expandable>\n")
	sb.WriteString(fmt.Sprintf("<b>Cache hits (no API call):</b> <code>%d</code>\n", stats.CacheHits))
	sb.WriteString(fmt.Sprintf("<b>Resolved via API:</b> <code>%d</code>\n", stats.APISuccess))
	sb.WriteString(fmt.Sprintf("<b>API failures:</b> <code>%d</code>\n", stats.APIFailed))
	sb.WriteString(fmt.Sprintf("<b>Fell back to yt-dlp:</b> <code>%d</code>\n", stats.FallbackToYtDlp))
	sb.WriteString(fmt.Sprintf("<b>Avg resolve time:</b> <code>%s</code>\n\n", stats.AvgResolveTime.Round(10*time.Millisecond)))

	sb.WriteString(fmt.Sprintf(
		"<b>Search fallback:</b> <code>%d</code> attempts, <code>%d</code> failed (%.1f%% success)\n\n",
		stats.SearchAttempts, stats.SearchFailed, stats.SearchSuccessRate(),
	))

	sb.WriteString(fmt.Sprintf("<b>Last success:</b> %s\n", tgTime(stats.LastSuccessAt)))
	sb.WriteString(fmt.Sprintf("<b>Last failure:</b> %s\n", tgTime(stats.LastFailureAt)))
	if stats.LastFailureMsg != "" {
		sb.WriteString(fmt.Sprintf("<b>Last error:</b> <code>%s</code>\n", truncate(stats.LastFailureMsg, 200)))
	}
	sb.WriteString(fmt.Sprintf("\n<b>Tracking since:</b> %s\n", getFormattedDuration(time.Since(stats.StartedAt))))
	sb.WriteString("</blockquote>\n\n")
	sb.WriteString("<i>Use /yt reset to clear these counters.</i>")

	_, err := replyRich(c, m, sb.String(), nil)
	return err
}
