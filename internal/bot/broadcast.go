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
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	td "github.com/AshokShau/gotdbot"
)

var (
	broadcastCancelFlag atomic.Bool
	broadcastInProgress atomic.Bool
)

// isUserGoneError reports whether err means the target user blocked the bot
// (isBlocked) or no longer has an account (isDeleted). Matching is on the
// lower-cased error text because TDLib's descriptions are not consistently
// cased and the error type exposes no stable code here.
func isUserGoneError(err error) (isBlocked, isDeleted bool) {
	if err == nil {
		return false, false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "user_is_blocked"), strings.Contains(msg, "bot was blocked by the user"):
		return true, false
	case strings.Contains(msg, "user_is_deleted"), strings.Contains(msg, "user_deactivated"), strings.Contains(msg, "user is deactivated"):
		return false, true
	// TDLib descriptions seen on DM targets with no usable chat (never
	// started the bot, or the chat was cleared). Resending will keep failing,
	// so they are treated as "blocked" for skip purposes.
	case strings.Contains(msg, "chat not found"),
		strings.Contains(msg, "have no write access to the chat"),
		strings.Contains(msg, "can't initiate conversation with a user"):
		return true, false
	default:
		return false, false
	}
}

// isChatGoneError reports whether err means the target chat is no longer
// reachable (bot kicked/left, chat deleted, or otherwise inaccessible).
func isChatGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"chat_write_forbidden", "channel_private", "user_not_participant",
		"peer_id_invalid", "chat_id_invalid", "bot was kicked",
		"chat not found", "have no write access to the chat",
		"group chat was deleted",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// deadTargets collects unreachable targets during a broadcast so they are
// flagged in the database in bulk instead of one write per failed send.
type deadTargets struct {
	chats, blocked, deleted []int64
}

const deadFlushSize = 200

func (d *deadTargets) pending() int {
	return len(d.chats) + len(d.blocked) + len(d.deleted)
}

func (d *deadTargets) flush() {
	if d.pending() == 0 {
		return
	}
	if len(d.chats) > 0 {
		_ = db.Instance.MarkChatsInvalid(d.chats)
	}
	if len(d.blocked) > 0 {
		_ = db.Instance.MarkUsersBlocked(d.blocked)
	}
	if len(d.deleted) > 0 {
		_ = db.Instance.MarkUsersDeleted(d.deleted)
	}
	*d = deadTargets{}
}

func getFloodWait(err error) int {
	if err == nil {
		return 0
	}

	type retryError interface {
		GetRetryAfter() int
	}

	if re, ok := err.(retryError); ok {
		return re.GetRetryAfter()
	}

	if tdErr, ok := err.(*td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	if tdErr, ok := err.(td.Error); ok {
		return tdErr.GetRetryAfter()
	}

	return 0
}

func cancelBroadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if !broadcastInProgress.Load() {
		_, _ = m.ReplyText(c, "No broadcast in progress.", nil)
		return td.EndGroups
	}

	broadcastCancelFlag.Store(true)
	_, _ = m.ReplyText(c, "Broadcast stopped.", nil)
	return td.EndGroups
}

func broadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	if broadcastInProgress.Load() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	reply, err := m.GetRepliedMessage(c)
	if err != nil {
		usage := `Please reply to a message to broadcast.

Usage:
-chat  : groups only
-user  : users only
-both  : groups + users (default)
-copy  : send as copy

Examples:
/broadcast
/broadcast -chat
/broadcast -user -copy
`

		_, _ = m.ReplyText(c, usage, nil)
		return td.EndGroups
	}

	args := strings.Fields(Args(m))

	copyMode := false
	mode := "both" // default

	for _, a := range args {
		switch a {
		case "-copy":
			copyMode = true
		case "-chat":
			mode = "chat"
		case "-user":
			mode = "user"
		case "-both":
			mode = "both"
		}
	}

	// Use the active (not blocked/deleted/invalid) lists so we don't burn time
	// re-sending to targets already known to be unreachable.
	chats, _ := db.Instance.GetActiveChats()
	users, _ := db.Instance.GetActiveUsers()

	groupsMap := make(map[int64]bool)
	for _, id := range chats {
		groupsMap[id] = true
	}

	var targets []int64

	switch mode {
	case "chat":
		targets = append(targets, chats...)
	case "user":
		targets = append(targets, users...)
	case "both":
		targets = append(targets, chats...)
		targets = append(targets, users...)
	}

	if len(targets) == 0 {
		_, _ = m.ReplyText(c, "No targets found.", nil)
		return td.EndGroups
	}

	broadcastCancelFlag.Store(false)
	broadcastInProgress.Store(true)

	sentMsg, _ := m.ReplyText(c, "Broadcast started.", nil)

	go func() {
		defer broadcastInProgress.Store(false)

		var failedBuilder strings.Builder
		var dead deadTargets
		count, ucount, skipped := 0, 0, 0
		defer dead.flush()

		for _, chatID := range targets {
			if broadcastCancelFlag.Load() {
				dead.flush()
				_, _ = sentMsg.EditText(
					c,
					fmt.Sprintf("Broadcast stopped.\nGroups: %d\nUsers: %d\nSkipped (blocked/deleted/invalid): %d", count, ucount, skipped),
					nil,
				)
				return
			}

			var errSend error
			if copyMode {
				_, errSend = reply.Copy(c, chatID, &td.CopyMessageOpts{
					ReplyMarkup: reply.ReplyMarkup,
				})
			} else {
				_, errSend = reply.Forward(c, chatID, &td.ForwardMessageOpts{})
			}

			if errSend == nil {
				if groupsMap[chatID] {
					count++
				} else {
					ucount++
				}
				time.Sleep(200 * time.Millisecond)
			} else {
				wait := getFloodWait(errSend)
				if wait > 0 {
					time.Sleep(time.Duration(wait+30) * time.Second)
					continue
				}

				if groupsMap[chatID] {
					if isChatGoneError(errSend) {
						dead.chats = append(dead.chats, chatID)
						skipped++
						if dead.pending() >= deadFlushSize {
							dead.flush()
						}
						continue
					}
				} else if blocked, deleted := isUserGoneError(errSend); blocked || deleted {
					if blocked {
						dead.blocked = append(dead.blocked, chatID)
					} else {
						dead.deleted = append(dead.deleted, chatID)
					}
					skipped++
					if dead.pending() >= deadFlushSize {
						dead.flush()
					}
					continue
				}

				failedBuilder.WriteString(fmt.Sprintf("%d - %v\n", chatID, errSend))
			}
		}

		dead.flush()
		text := fmt.Sprintf("Broadcast ended.\nGroups: %d\nUsers: %d\nSkipped (blocked/deleted/invalid): %d", count, ucount, skipped)
		failedStr := failedBuilder.String()

		if failedStr != "" {
			errFile := filepath.Join(
				os.TempDir(),
				fmt.Sprintf("errors_%d.txt", time.Now().UnixNano()),
			)

			if err := os.WriteFile(errFile, []byte(failedStr), 0644); err == nil {
				defer os.Remove(errFile)

				_, errSendDoc := m.ReplyDocument(
					c,
					td.InputFileLocal{Path: errFile},
					&td.SendDocumentOpts{Caption: text},
				)

				if errSendDoc != nil {
					_, _ = sentMsg.EditText(c, text, nil)
				}
			} else {
				_, _ = sentMsg.EditText(c, text, nil)
			}
		} else {
			_, _ = sentMsg.EditText(c, text, nil)
		}
	}()

	return td.EndGroups
}
