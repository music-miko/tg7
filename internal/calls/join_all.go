/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package calls

import (
	"time"

	td "github.com/AshokShau/gotdbot"
)

const joinAllDelay = 2 * time.Second

// AssistantReport is the health of one assistant plus the outcome of trying
// to join it into a chat. Assistants that are frozen, revoked or unreachable
// are reported but not joined.
type AssistantReport struct {
	AssistantStatus
	Attempted bool
	JoinErr   error
}

// Joined reports whether the assistant was joined into the chat successfully.
func (r AssistantReport) Joined() bool {
	return r.Attempted && r.JoinErr == nil
}

// JoinAllAssistants checks every configured assistant and makes each usable
// one join chatID, using the normal invite-link flow. Joins are sequential
// with a short pause so they don't trip Telegram's rate limits on the shared
// invite link. Frozen, revoked and unreachable assistants (including ones
// skipped at startup) are included in the result with their state so the
// caller can show which session needs replacing.
func (c *TelegramCalls) JoinAllAssistants(bot *td.Client, chatID int64) []AssistantReport {
	statuses := c.CheckAssistants()
	reports := make([]AssistantReport, 0, len(statuses))

	joinedAny := false
	for _, status := range statuses {
		report := AssistantReport{AssistantStatus: status}
		if status.Index >= 0 && status.Usable() {
			c.mu.RLock()
			var acc *AssistantAccount
			if status.Index < len(c.accounts) {
				acc = c.accounts[status.Index]
			}
			c.mu.RUnlock()

			if acc != nil {
				if joinedAny {
					time.Sleep(joinAllDelay)
				}
				report.Attempted = true
				report.JoinErr = c.ensureAssistantMember(bot, chatID, acc, status.Index)
				joinedAny = true
			}
		}
		reports = append(reports, report)
	}

	return reports
}
