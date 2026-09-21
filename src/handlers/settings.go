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
	"strings"

	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/core/db"

	td "github.com/AshokShau/gotdbot"
)

// settingsButtons builds the native toggle button rows for the /settings
// panel (Bot API 10.3 Button Revolution - see richtext.go). Each button
// carries its own current-state label and is styled to reflect it, so the
// two-button "label -> value" rows the old trailing reply_markup keyboard
// needed collapse into one full-width button per setting.
func settingsButtons(playModeStr, adminMode string, cmdDelete bool, language string) []RichButton {
	playStyle := ButtonStyleDefault
	if playModeStr == utils.Admins {
		playStyle = ButtonStylePrimary
	}

	deleteStyle := ButtonStyleDanger
	deleteLabel := "🗑 Command Delete: Off"
	if cmdDelete {
		deleteStyle = ButtonStyleSuccess
		deleteLabel = "🗑 Command Delete: On"
	}

	adminStyle := ButtonStyleDefault
	if adminMode == utils.Admins {
		adminStyle = ButtonStylePrimary
	}

	langLabel := "🌐 Language: English"
	if language != "" && language != "en" {
		langLabel = "🌐 Language: " + language
	}

	return []RichButton{
		{Text: fmt.Sprintf("🎚 Play Mode: %s", playModeStr), Style: playStyle, Data: "settings_play"},
		{Text: deleteLabel, Style: deleteStyle, Data: "settings_delete"},
		{Text: fmt.Sprintf("🛡 Admin Mode: %s", adminMode), Style: adminStyle, Data: "settings_admin"},
		{Text: langLabel, Style: ButtonStyleDefault, Data: "settings_lang"},
	}
}

// settingsRichMessage builds the native-button /settings panel as an
// InputRichMessage: a heading, an instruction line, one full-width toggle
// button per setting, and a Close button.
func settingsRichMessage(chatTitle, playModeStr, adminMode string, cmdDelete bool, language string) *td.InputRichMessage {
	blocks := []td.InputPageBlock{
		td.InputPageBlockSectionHeading{Size: 3, Text: td.RichTextPlain{Text: chatTitle + " settings"}},
		td.InputPageBlockParagraph{Text: td.RichTextPlain{Text: "Tap a button below to change this chat's current settings."}},
	}

	for _, b := range settingsButtons(playModeStr, adminMode, cmdDelete, language) {
		blocks = append(blocks, buttonRow(b))
	}
	blocks = append(blocks, buttonRow(RichButton{Text: "Close", Style: ButtonStyleDanger, Data: "vcplay_close"}))

	return &td.InputRichMessage{
		DetectAutomaticBlocks: true,
		Source:                td.RichMessageSourceBlocks{Blocks: blocks},
	}
}

func settingsHandler(c *td.Client, m *td.Message) error {
	if m.IsPrivate() {
		return nil
	}

	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId
	admins, err := cache.GetAdmins(c, chatID, false)
	if err != nil {
		return err
	}

	// Check if user is admin
	var isAdmin bool
	for _, admin := range admins {
		if SenderID(admin.MemberId) == m.SenderID() {
			isAdmin = true
			break
		}
	}

	if !isAdmin {
		return nil
	}

	// Get current settings
	getPlayMode := db.Instance.GetPlayMode(chatID)
	playModeStr := utils.Everyone
	if getPlayMode {
		playModeStr = utils.Admins
	}
	getAdminMode := db.Instance.GetAdminMode(chatID)
	cmdDelete := db.Instance.GetCmdDelete(chatID)
	language, _ := db.Instance.GetLanguage(chatID)

	chat, err := m.GetChat(c)
	if err != nil {
		c.Logger.Warn("Failed to get chat", "error", err)
		return nil
	}

	_, err = m.ReplyRichMessage(c, settingsRichMessage(chat.Title, playModeStr, getAdminMode, cmdDelete, language), nil)
	return err
}

func settingsCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if cb.IsPrivate() {
		return nil
	}

	chatID := cb.ChatId

	// Check admin permissions
	admins, err := cache.GetAdmins(c, chatID, false)
	if err != nil {
		return err
	}

	var hasPerms bool
	for _, admin := range admins {
		if SenderID(admin.MemberId) == cb.SenderUserId {
			if _, isCreator := admin.Status.(*td.ChatMemberStatusCreator); isCreator {
				hasPerms = true
				break
			}
			rights, _ := cache.GetRights(c, chatID, cb.SenderUserId, false)
			hasPerms = rights != nil && rights.CanManageVideoChats
			break
		}
	}

	if !hasPerms {
		err = cb.Answer(c, 0, true, "You don't have permission to change settings.", "")
		return err
	}

	// Process the callback data
	data := cb.DataString()
	if data == "settings_main" {
		return cb.Answer(c, 0, false, "Update your chat settings", "")
	}

	parts := strings.Split(data, "_")
	if len(parts) < 2 {
		return nil
	}

	settingType := parts[1]

	switch settingType {
	case "delete":
		cmdDelete := db.Instance.GetCmdDelete(chatID)
		_ = db.Instance.SetCmdDelete(chatID, !cmdDelete)
	case "play":
		getPlayMode := db.Instance.GetPlayMode(chatID)
		_ = db.Instance.SetPlayMode(chatID, !getPlayMode)
	case "admin":
		getAdminMode := db.Instance.GetAdminMode(chatID)
		newMode := utils.Everyone
		if getAdminMode == utils.Everyone {
			newMode = utils.Admins
		}
		_ = db.Instance.SetAdminMode(chatID, newMode)
	case "lang":
		return cb.Answer(c, 0, true, "Language selection is not yet implemented via this menu.", "")
	default:
		return cb.Answer(c, 0, true, "Unknown setting", "")
	}

	getPlayMode := db.Instance.GetPlayMode(chatID)
	playModeStr := utils.Everyone
	if getPlayMode {
		playModeStr = utils.Admins
	}
	getAdminMode := db.Instance.GetAdminMode(chatID)
	cmdDelete := db.Instance.GetCmdDelete(chatID)
	language, _ := db.Instance.GetLanguage(chatID)

	chat, err := c.GetChat(chatID)
	if err != nil {
		c.Logger.Warn("Failed to get chat", "error", err)
		return nil
	}

	content := &td.InputMessageRichMessage{Message: settingsRichMessage(chat.Title, playModeStr, getAdminMode, cmdDelete, language)}
	_, err = c.EditMessageText(chatID, content, cb.MessageId, &td.EditMessageTextOpts{})
	if err != nil {
		return err
	}

	_ = cb.Answer(c, 0, false, "Settings updated", "")
	return nil
}
