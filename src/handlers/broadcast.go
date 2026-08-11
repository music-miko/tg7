/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/core/db"
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

// isUserGoneError reports whether err indicates the target user has blocked
// the bot (isBlocked=true) or their account no longer exists (isBlocked=false,
// meaning deleted). Matching is done on the error text, same convention used
// elsewhere in this codebase (see vc/userbot.go, vc/leave_all.go) since the
// underlying td/MTProto error type doesn't expose a stable error code here.
func isUserGoneError(err error) (isBlocked, isDeleted bool) {
	if err == nil {
		return false, false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "USER_IS_BLOCKED"), strings.Contains(msg, "bot was blocked by the user"):
		return true, false
	case strings.Contains(msg, "USER_IS_DELETED"), strings.Contains(msg, "USER_DEACTIVATED"), strings.Contains(msg, "user is deactivated"):
		return false, true
	default:
		return false, false
	}
}

// isChatGoneError reports whether err indicates the target chat is no longer
// reachable (bot kicked/left, chat deleted, or otherwise inaccessible).
func isChatGoneError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "CHAT_WRITE_FORBIDDEN"),
		strings.Contains(msg, "CHANNEL_PRIVATE"),
		strings.Contains(msg, "USER_NOT_PARTICIPANT"),
		strings.Contains(msg, "PEER_ID_INVALID"),
		strings.Contains(msg, "CHAT_ID_INVALID"),
		strings.Contains(msg, "bot was kicked"),
		strings.Contains(msg, "chat not found"),
		strings.Contains(msg, "group chat was deleted"):
		return true
	default:
		return false
	}
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

	// Use the active (non-blocked/deleted/invalid) lists so we don't burn
	// time and flood-wait budget re-sending to targets already known to be
	// unreachable.
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
		count, ucount, skipped := 0, 0, 0

		for _, chatID := range targets {
			if broadcastCancelFlag.Load() {
				_, _ = sentMsg.EditText(
					c,
					fmt.Sprintf("Broadcast stopped.\nGroups: %d\nUsers: %d\nSkipped (blocked/deleted/invalid): %d", count, ucount, skipped),
					nil,
				)
				return
			}

			var errSend error
			if copyMode {
				_, errSend = reply.Copy(c, chatID, &td.SendCopyOpts{
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

				isGroup := groupsMap[chatID]
				if isGroup && isChatGoneError(errSend) {
					_ = db.Instance.MarkChatInvalid(chatID)
					skipped++
					continue
				}
				if !isGroup {
					if blocked, deleted := isUserGoneError(errSend); blocked || deleted {
						if blocked {
							_ = db.Instance.MarkUserBlocked(chatID)
						} else {
							_ = db.Instance.MarkUserDeleted(chatID)
						}
						skipped++
						continue
					}
				}

				failedBuilder.WriteString(fmt.Sprintf("%d - %v\n", chatID, errSend))
			}
		}

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
