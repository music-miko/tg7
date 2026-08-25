/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/core/cache"
	"fmt"
	"time"

	td "github.com/AshokShau/gotdbot"
)

// supergroupNoticeButtons builds the native button row for the "please
// convert to a supergroup" notice (Bot API 10.3 Button Revolution - see
// richtext.go): Add Me styled primary, Help default, Channel/Group link
// buttons, replacing the old trailing core.AddMeMarkup reply_markup.
func supergroupNoticeButtons(username string) []td.InputPageBlock {
	return []td.InputPageBlock{
		buttonRow(RichButton{Text: "➕ Add me to your group", Style: ButtonStylePrimary, Url: fmt.Sprintf("https://t.me/%s?startgroup=true", username)}),
		buttonRow(RichButton{Text: "Help", Style: ButtonStyleDefault, Data: "help_all"}),
		buttonRow(
			RichButton{Text: "Updates", Style: ButtonStyleLink, Url: config.SupportChannel},
			RichButton{Text: "Group", Style: ButtonStyleLink, Url: config.SupportGroup},
		),
	}
}

func handleVoiceChatMessage(c *td.Client, update *td.UpdateNewMessage) error {
	m := update.Message
	chatID := m.ChatId

	if m.IsGroup() {
		warning := fmt.Sprintf("This chat (%d) is not a supergroup yet.\n⚠️ Please convert this chat to a supergroup and add me as admin.", chatID)
		guide := "If you don't know how to convert, use this guide:\n🔗 https://te.legra.ph/How-to-Convert-a-Group-to-a-Supergroup-01-02\n\nIf you have any questions, join our support group:"

		blocks := []td.InputPageBlock{
			td.InputPageBlockParagraph{Text: td.RichTextPlain{Text: warning}},
			td.InputPageBlockParagraph{Text: td.RichTextPlain{Text: guide}},
		}
		blocks = append(blocks, supergroupNoticeButtons(c.Me.Usernames.EditableUsername)...)

		_, _ = c.SendRichMessage(chatID, &td.InputRichMessage{
			DetectAutomaticBlocks: true,
			Source:                td.RichMessageSourceBlocks{Blocks: blocks},
		}, &td.SendTextMessageOpts{DisableWebPagePreview: true})

		time.Sleep(1 * time.Second)
		_ = c.LeaveChat(chatID)
		return nil
	}

	if m.Content == nil {
		return nil
	}
	var message string
	switch m.Content.(type) {
	case *td.MessageVideoChatStarted:
		cache.ChatCache.ClearChat(chatID)
		message = "🎙️ Video chat started!\nUse /play <song name> to play music."
	case *td.MessageVideoChatEnded:
		cache.ChatCache.ClearChat(chatID)
		message = "🎧 Video chat ended!\nAll queues cleared."
	default:
		return nil
	}

	_, _ = c.SendTextMessage(chatID, message, nil)
	return td.EndGroups
}
