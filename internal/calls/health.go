/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package calls

import (
	"strings"
	"time"

	tg "github.com/xAmarnath/gogram/telegram"
)

// AssistantState describes whether an assistant can currently be used.
type AssistantState int

const (
	AssistantActive AssistantState = iota
	AssistantFrozen
	AssistantDead
	AssistantUnreachable
)

// AssistantStatus is the health of one configured assistant slot.
type AssistantStatus struct {
	Slot        string
	Index       int
	UserID      int64
	Username    string
	State       AssistantState
	Detail      string
	FrozenSince time.Time
	FrozenUntil time.Time
	AppealURL   string
}

// Usable reports whether the assistant can take part in calls.
func (s AssistantStatus) Usable() bool {
	return s.State == AssistantActive
}

type freezeInfo struct {
	Frozen bool
	Since  time.Time
	Until  time.Time
	Appeal string
}

func parseFreeze(appConfig tg.HelpAppConfig) freezeInfo {
	var info freezeInfo
	cfg, ok := appConfig.(*tg.HelpAppConfigObj)
	if !ok || cfg == nil {
		return info
	}
	obj, ok := cfg.Config.(*tg.JsonObject)
	if !ok || obj == nil {
		return info
	}
	for _, entry := range obj.Value {
		if entry == nil {
			continue
		}
		switch entry.Key {
		case "freeze_since_date":
			info.Frozen = true
			if n, ok := entry.Value.(*tg.JsonNumber); ok {
				info.Since = time.Unix(int64(n.Value), 0).UTC()
			}
		case "freeze_until_date":
			if n, ok := entry.Value.(*tg.JsonNumber); ok {
				info.Until = time.Unix(int64(n.Value), 0).UTC()
			}
		case "freeze_appeal_url":
			if s, ok := entry.Value.(*tg.JsonString); ok {
				info.Appeal = s.Value
			}
		}
	}
	return info
}

func isDeadSessionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	for _, marker := range []string{
		"AUTH_KEY_UNREGISTERED",
		"AUTH_KEY_INVALID",
		"AUTH_KEY_DUPLICATED",
		"SESSION_REVOKED",
		"SESSION_EXPIRED",
		"USER_DEACTIVATED",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (c *TelegramCalls) recordSkipped(status AssistantStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.skipped {
		if existing.Slot == status.Slot {
			c.skipped[i] = status
			return
		}
	}
	c.skipped = append(c.skipped, status)
}

func inspectAccount(acc *AssistantAccount) AssistantStatus {
	status := AssistantStatus{Slot: acc.slot, State: AssistantActive}
	if acc.self != nil {
		status.UserID = acc.self.ID
		status.Username = acc.self.Username
	}

	appConfig, err := acc.App.HelpGetAppConfig(0)
	switch {
	case err == nil:
		if freeze := parseFreeze(appConfig); freeze.Frozen {
			status.State = AssistantFrozen
			status.FrozenSince = freeze.Since
			status.FrozenUntil = freeze.Until
			status.AppealURL = freeze.Appeal
			status.Detail = "account is frozen by Telegram"
		}
	case isDeadSessionError(err):
		status.State = AssistantDead
		status.Detail = err.Error()
	default:
		status.State = AssistantUnreachable
		status.Detail = err.Error()
	}
	return status
}

// CheckAssistants returns the live health of every configured assistant:
// the running ones are re-checked against Telegram right now, and the ones
// that were skipped at startup (frozen, revoked session) are listed as well,
// ordered by their configured slot.
func (c *TelegramCalls) CheckAssistants() []AssistantStatus {
	c.mu.RLock()
	accounts := make([]*AssistantAccount, len(c.accounts))
	copy(accounts, c.accounts)
	skipped := make([]AssistantStatus, len(c.skipped))
	copy(skipped, c.skipped)
	c.mu.RUnlock()

	statuses := make([]AssistantStatus, 0, len(accounts)+len(skipped))
	for index, acc := range accounts {
		if acc == nil || acc.App == nil {
			continue
		}
		status := inspectAccount(acc)
		status.Index = index
		statuses = append(statuses, status)
	}
	for _, status := range skipped {
		status.Index = -1
		statuses = append(statuses, status)
	}
	return statuses
}
