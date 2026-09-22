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
	"os"
	"strings"
	"time"

	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/db"

	td "github.com/AshokShau/gotdbot"
)

// deleteBackupMarkup is the "🗑 Delete Backup" inline button attached to
// every backup document (manual or automatic), letting a dev clean it up
// from the chat without needing message-delete rights of their own.
var deleteBackupMarkup = &td.ReplyMarkupInlineKeyboard{
	Rows: [][]td.InlineKeyboardButton{
		{
			{
				Text:  "🗑 Delete Backup",
				Type:  &td.InlineKeyboardButtonTypeCallback{Data: []byte("backup_delete")},
				Style: td.ButtonStyleDanger{},
			},
		},
	},
}

// StartDailyBackups runs forever in the background, taking a full Mongo
// backup once a day at config.BackupHourUTC (UTC) and sending the resulting
// zip to config.BackupChatId. Call this once from Init() after the bot
// client is ready.
func StartDailyBackups(c *td.Client) {
	if config.BackupChatId == 0 {
		c.Logger.Info("[Backup] BACKUP_CHAT_ID is 0, daily Mongo backups are disabled")
		return
	}

	go func() {
		for {
			wait := durationUntilNextBackup(time.Now().UTC(), config.BackupHourUTC)
			c.Logger.Info("[Backup] next daily backup scheduled", "in", wait.Round(time.Minute).String())
			time.Sleep(wait)

			runBackup(c, config.BackupChatId, "Daily automatic backup")
		}
	}()
}

// durationUntilNextBackup returns how long to sleep until the next
// occurrence of hourUTC (0-23), always at least a minute away so a restart
// right at the target hour doesn't loop-fire.
func durationUntilNextBackup(now time.Time, hourUTC int) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), hourUTC, 0, 0, 0, time.UTC)
	if !next.After(now.Add(time.Minute)) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}

// backupCaption builds the HTML caption sent alongside a backup zip.
func backupCaption(label string, result *db.BackupResult) string {
	var body strings.Builder
	body.WriteString(fmt.Sprintf("<b>📦 %s</b>\n\n", label))
	body.WriteString(fmt.Sprintf(
		"<b>Collections:</b> %d\n<b>Documents:</b> %d\n<b>Took:</b> %s",
		len(result.Collections), result.TotalDocuments(), result.Finished.Sub(result.StartedAt).Round(time.Second),
	))

	if result.HasErrors() {
		body.WriteString("\n\n⚠️ <b>Some collections failed:</b>")
		for _, coll := range result.Collections {
			if coll.Err != nil {
				body.WriteString(fmt.Sprintf("\n- <code>%s</code>: %s", html.EscapeString(coll.Collection), html.EscapeString(coll.Err.Error())))
			}
		}
	}

	body.WriteString("\n\nReply to this file with /restore to restore this data.")
	return body.String()
}

// runBackup performs one backup run and sends the resulting zip (with a
// Delete Backup button on success, or a plain error report on failure) to
// destChatId.
func runBackup(c *td.Client, destChatId int64, label string) {
	result, err := db.Instance.BackupToZip()
	if err != nil {
		c.Logger.Error("[Backup] backup failed", "error", err.Error())
		_, _ = c.SendTextMessage(destChatId, fmt.Sprintf("<b>%s failed:</b> <code>%s</code>", label, html.EscapeString(err.Error())), &td.SendTextMessageOpts{ParseMode: "HTML"})
		return
	}
	defer os.Remove(result.Path)

	_, sendErr := c.SendDocument(destChatId, td.InputFileLocal{Path: result.Path}, &td.SendDocumentOpts{
		Caption:     backupCaption(label, result),
		ParseMode:   "HTML",
		ReplyMarkup: deleteBackupMarkup,
	})
	if sendErr != nil {
		c.Logger.Error("[Backup] failed to send backup zip", "error", sendErr.Error(), "chat_id", destChatId)
		_, _ = c.SendTextMessage(destChatId, fmt.Sprintf("<b>%s:</b> backup was created but failed to upload: <code>%s</code>", label, html.EscapeString(sendErr.Error())), &td.SendTextMessageOpts{ParseMode: "HTML"})
		return
	}

	c.Logger.Info("[Backup] completed", "documents", result.TotalDocuments(), "collections", len(result.Collections))
}

// backupDeleteCallbackHandler handles the "🗑 Delete Backup" button on a
// backup message. Dev-only, same as the commands that produce it.
func backupDeleteCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	isDevUser := false
	for _, dev := range config.DEVS {
		if dev == cb.SenderUserId {
			isDevUser = true
			break
		}
	}

	if !isDevUser {
		_ = cb.Answer(c, 0, true, "Only devs can do that.", "")
		return nil
	}

	if err := c.DeleteMessages(cb.ChatId, []int64{cb.MessageId}, &td.DeleteMessagesOpts{Revoke: true}); err != nil {
		_ = cb.Answer(c, 0, true, fmt.Sprintf("Failed to delete: %s", err.Error()), "")
		return nil
	}

	_ = cb.Answer(c, 0, false, "Backup deleted.", "")
	return nil
}

// backupHandler handles /backup: an on-demand version of the daily job,
// sent back into the chat the command was used in so a dev can verify it
// works without waiting for the scheduled run.
func backupHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	status, err := m.ReplyText(c, "Backing up the database, this may take a moment...", nil)
	if err != nil {
		return td.EndGroups
	}

	result, err := db.Instance.BackupToZip()
	if err != nil {
		_, _ = status.EditText(c, fmt.Sprintf("Backup failed: %s", err.Error()), nil)
		return td.EndGroups
	}
	defer os.Remove(result.Path)

	_, err = m.ReplyDocument(c, td.InputFileLocal{Path: result.Path}, &td.SendDocumentOpts{
		Caption:     backupCaption("Manual backup", result),
		ParseMode:   "HTML",
		ReplyMarkup: deleteBackupMarkup,
	})
	if err != nil {
		_, _ = status.EditText(c, fmt.Sprintf("Backup created but failed to upload: %s", err.Error()), nil)
		return td.EndGroups
	}

	_ = c.DeleteMessages(m.ChatId, []int64{status.Id}, &td.DeleteMessagesOpts{Revoke: true})
	return td.EndGroups
}

// restoreHandler handles /restore. It must be used as a reply to a .zip
// document previously produced by /backup or the daily automatic backup.
// It is intentionally destructive (drop + reinsert per collection) so it
// should only ever be reachable by devs.
func restoreHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	reply, err := m.GetRepliedMessage(c)
	if err != nil || reply == nil {
		_, _ = m.ReplyText(c, "Reply to a backup .zip file with /restore to restore the database from it.", nil)
		return td.EndGroups
	}

	file, fileName := getFile(reply)
	if file == nil {
		_, _ = m.ReplyText(c, "That's not a document. Reply to the backup .zip file with /restore.", nil)
		return td.EndGroups
	}

	if !strings.HasSuffix(strings.ToLower(fileName), ".zip") {
		_, _ = m.ReplyText(c, "That file doesn't look like a backup .zip. Reply to the backup .zip file with /restore.", nil)
		return td.EndGroups
	}

	status, err := m.ReplyText(c, "⚠️ Restoring the database from this backup now. This will drop and replace every collection found in the file.", nil)
	if err != nil {
		return td.EndGroups
	}

	downloaded, err := reply.Download(c, 1, 0, 0, true)
	if err != nil {
		_, _ = status.EditText(c, fmt.Sprintf("Failed to download backup file: %s", err.Error()), nil)
		return td.EndGroups
	}
	defer os.Remove(downloaded.Local.Path)

	result, err := db.Instance.RestoreFromZip(downloaded.Local.Path)
	if err != nil {
		_, _ = status.EditText(c, fmt.Sprintf("Restore failed: %s", err.Error()), nil)
		return td.EndGroups
	}

	var sb strings.Builder
	sb.WriteString("<b>✅ Restore complete</b>\n\n")
	sb.WriteString(fmt.Sprintf("<b>Collections restored:</b> %d\n<b>Documents:</b> %d\n<b>Took:</b> %s\n",
		len(result.Collections), result.TotalDocuments(), result.Finished.Sub(result.StartedAt).Round(time.Second)))

	if result.HasErrors() {
		sb.WriteString("\n<b>⚠️ Some collections failed:</b>\n")
		for _, coll := range result.Collections {
			if coll.Err != nil {
				sb.WriteString(fmt.Sprintf("- <code>%s</code>: %s\n", html.EscapeString(coll.Collection), html.EscapeString(coll.Err.Error())))
			}
		}
	}

	_, _ = status.EditText(c, sb.String(), &td.EditTextMessageOpts{ParseMode: "HTML"})
	return td.EndGroups
}
