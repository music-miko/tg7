/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"fmt"
	"html"
	"strings"
	"time"

	"ashokshau/tgmusic/internal/downloader"

	td "github.com/AshokShau/gotdbot"
)

// tgTime renders t as a Rich HTML <tg-time> element (client-local,
// auto-updating timestamp), falling back to a plain "never" for the zero
// value.
func tgTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmt.Sprintf("<tg-time unix=\"%d\">%s</tg-time>", t.Unix(), html.EscapeString(t.Format("Jan 2, 15:04 MST")))
}

// arcTableRow renders one <tr> of the Arc stats table.
func arcTableRow(label string, attempts, ok, fail int64, rate float64) string {
	return fmt.Sprintf(
		"<tr><td align=\"left\">%s</td><td align=\"right\">%d</td><td align=\"right\">%d</td><td align=\"right\">%d</td><td align=\"right\">%.1f%%</td></tr>",
		label, attempts, ok, fail, rate,
	)
}

// ytStatsHandler handles the /yt command: a developer-facing dashboard of
// ArcMusic API download/search success rates, rendered as a real HTML table
// (Rich Messages, see https://core.telegram.org/bots/api#rich-messages)
// plus an expandable blockquote for the finer-grained details.
//
// Usage:
//
//	/yt         - show the dashboard
//	/yt reset   - clear the counters and restart tracking
func ytStatsHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	args := strings.Fields(Args(m))
	if len(args) > 0 && strings.EqualFold(args[0], "reset") {
		downloader.ResetArcStats()
		_, err := m.ReplyText(c, "✅ ArcMusic API stats have been reset.", nil)
		return err
	}

	stats := downloader.GetArcStats()

	var sb strings.Builder
	sb.WriteString("<h3>🎧 ArcMusic API Stats</h3>")

	sb.WriteString("<table bordered striped>")
	sb.WriteString("<tr><th>Kind</th><th>Total</th><th>OK</th><th>Fail</th><th>Rate</th></tr>")
	sb.WriteString(arcTableRow("Audio", stats.AudioAttempts, stats.AudioSuccess, stats.AudioFailed, stats.AudioSuccessRate()))
	sb.WriteString(arcTableRow("Video", stats.VideoAttempts, stats.VideoSuccess, stats.VideoFailed, stats.VideoSuccessRate()))
	sb.WriteString(arcTableRow("Overall", stats.TotalAttempts(), stats.TotalSuccess(), stats.TotalFailed(), stats.SuccessRate()))
	sb.WriteString("</table>")

	sb.WriteString("<blockquote expandable>")
	sb.WriteString(fmt.Sprintf("<b>Resolved via API:</b> <code>%d</code><br>", stats.APISuccess))
	sb.WriteString(fmt.Sprintf("<b>API failures:</b> <code>%d</code><br>", stats.APIFailed))
	sb.WriteString(fmt.Sprintf("<b>Fell back to yt-dlp:</b> <code>%d</code><br>", stats.FallbackToYtDlp))
	sb.WriteString(fmt.Sprintf("<b>Avg resolve time:</b> <code>%s</code><br><br>", stats.AvgResolveTime.Round(10*time.Millisecond)))

	sb.WriteString(fmt.Sprintf(
		"<b>Search fallback:</b> <code>%d</code> attempts, <code>%d</code> failed (%.1f%% success)<br><br>",
		stats.SearchAttempts, stats.SearchFailed, stats.SearchSuccessRate(),
	))

	sb.WriteString(fmt.Sprintf("<b>Last success:</b> %s<br>", tgTime(stats.LastSuccessAt)))
	sb.WriteString(fmt.Sprintf("<b>Last failure:</b> %s<br>", tgTime(stats.LastFailureAt)))
	if stats.LastFailureMsg != "" {
		sb.WriteString(fmt.Sprintf("<b>Last error:</b> <code>%s</code><br>", truncate(html.EscapeString(stats.LastFailureMsg), 200)))
	}
	sb.WriteString(fmt.Sprintf("<br><b>Tracking since:</b> %s<br>", getFormattedDuration(time.Since(stats.StartedAt))))
	sb.WriteString("</blockquote>")
	sb.WriteString("<i>Use /yt reset to clear these counters.</i>")

	richMessage := &td.InputRichMessage{Source: &td.RichMessageSourceHtml{Text: sb.String()}}
	_, err := m.ReplyRichMessage(c, richMessage, nil)
	return err
}
