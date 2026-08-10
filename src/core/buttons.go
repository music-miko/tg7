/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package core

import (
	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/utils"
	"fmt"

	"github.com/AshokShau/gotdbot"
)

func cb(text, data string) gotdbot.InlineKeyboardButton {
	return gotdbot.InlineKeyboardButton{
		Text: text,
		Type: &gotdbot.InlineKeyboardButtonTypeCallback{
			Data: []byte(data),
		},
	}
}

func url(text, link string) gotdbot.InlineKeyboardButton {
	return gotdbot.InlineKeyboardButton{
		Text: text,
		Type: &gotdbot.InlineKeyboardButtonTypeUrl{
			Url: link,
		},
	}
}

// urlPrimary is url() with the button's Style set to the dark-blue "primary"
// color introduced for InlineKeyboardButton, used to make our single most
// important call-to-action (Add to Group) stand out from the plain-styled
// buttons around it.
func urlPrimary(text, link string) gotdbot.InlineKeyboardButton {
	btn := url(text, link)
	btn.Style = gotdbot.ButtonStylePrimary{}
	return btn
}

var CloseBtn = cb("Close", "vcplay_close")
var HomeBtn = cb("Home", "help_back")
var HelpBtn = cb("Help", "help_all")
var UserBtn = cb("Users", "help_user")
var AdminBtn = cb("Admins", "help_admin")
var OwnerBtn = cb("Owner", "help_owner")
var DevsBtn = cb("Devs", "help_devs")
var PlaylistBtn = cb("Playlist", "help_playlist")
var AutoplayBtn = cb("Autoplay", "help_autoplay")

func SupportKeyboard() *gotdbot.ReplyMarkupInlineKeyboard {

	channelBtn := url("Updates", config.SupportChannel)
	groupBtn := url("Group", config.SupportGroup)

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{channelBtn, groupBtn},
			{CloseBtn},
		},
	}
}

func SupportBtn() *gotdbot.ReplyMarkupInlineKeyboard {
	channelBtn := url("Updates", config.SupportChannel)
	groupBtn := url("Group", config.SupportGroup)
	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{channelBtn, groupBtn},
		},
	}
}

func SettingsKeyboard(playMode, adminMode string, cmdDelete bool, language string) *gotdbot.ReplyMarkupInlineKeyboard {
	playText := "Everyone"
	if playMode == utils.Admins {
		playText = "Admins"
	}

	deleteText := "False"
	if cmdDelete {
		deleteText = "True"
	}

	adminText := "Everyone"
	if adminMode == utils.Admins {
		adminText = "Admins"
	}

	langText := "English"
	if language != "en" && language != "" {
		langText = language
	}

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{
				cb("Play Mode ➜", "settings_main"),
				cb(playText, "settings_play"),
			},
			{
				cb("Command Delete ➜", "settings_main"),
				cb(deleteText, "settings_delete"),
			},
			{
				cb("Admin Mode ➜", "settings_main"),
				cb(adminText, "settings_admin"),
			},
			{
				cb("Language ➜", "settings_main"),
				cb(langText, "settings_lang"),
			},
			{CloseBtn},
		},
	}
}

func HelpMenuKeyboard() *gotdbot.ReplyMarkupInlineKeyboard {

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{UserBtn, AdminBtn, OwnerBtn},
			{PlaylistBtn, DevsBtn, AutoplayBtn},
			{HomeBtn, CloseBtn},
		},
	}
}

func BackHelpMenuKeyboard() *gotdbot.ReplyMarkupInlineKeyboard {
	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{HelpBtn, HomeBtn},
			{CloseBtn},
		},
	}
}

func ControlButtons(mode string) *gotdbot.ReplyMarkupInlineKeyboard {
	skipBtn := cb("‣‣I", "play_skip")
	stopBtn := cb("▢", "play_stop")
	pauseBtn := cb("II", "play_pause")
	resumeBtn := cb("▷", "play_resume")
	muteBtn := cb("🔇", "play_mute")
	unmuteBtn := cb("🔊", "play_unmute")
	addToPlaylistBtn := cb("➕", "play_add_to_list")

	switch mode {

	case "play":
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{skipBtn, stopBtn, pauseBtn},
				{addToPlaylistBtn, CloseBtn},
			},
		}

	case "pause":
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{skipBtn, stopBtn, resumeBtn},
				{CloseBtn},
			},
		}

	case "resume":
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{skipBtn, stopBtn, pauseBtn},
				{CloseBtn},
			},
		}

	case "mute":
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{skipBtn, stopBtn, unmuteBtn},
				{CloseBtn},
			},
		}

	case "unmute":
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{skipBtn, stopBtn, muteBtn},
				{CloseBtn},
			},
		}

	default:
		return &gotdbot.ReplyMarkupInlineKeyboard{
			Rows: [][]gotdbot.InlineKeyboardButton{
				{CloseBtn},
			},
		}
	}
}

func AddMeMarkup(username string) *gotdbot.ReplyMarkupInlineKeyboard {

	addMeBtn := urlPrimary(
		"➕ Add me to your group",
		fmt.Sprintf("https://t.me/%s?startgroup=true", username),
	)

	channelBtn := url("Updates", config.SupportChannel)
	groupBtn := url("Group", config.SupportGroup)

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{addMeBtn},
			{HelpBtn},
			{channelBtn, groupBtn},
		},
	}
}

// SetupGuideBtn opens the step-by-step setup guide via callback.
var SetupGuideBtn = cb("Setup Guide", "setup_guide")

// StartBackBtn returns to the main /start panel via callback.
var StartBackBtn = cb("Back", "setup_back")

// PrivateStartMarkup builds the keyboard shown for /start in a private chat.
// Mirrors: Add to Group, Help & Commands, Support Chat / Updates, Setup Guide.
func PrivateStartMarkup(username string) *gotdbot.ReplyMarkupInlineKeyboard {
	addToGroupBtn := urlPrimary("➕ Add to Group", fmt.Sprintf("https://t.me/%s?startgroup=true", username))
	supportBtn := url("Support Chat", config.SupportGroup)
	updatesBtn := url("Updates", config.SupportChannel)

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{addToGroupBtn},
			{HelpBtn},
			{supportBtn, updatesBtn},
			{SetupGuideBtn},
		},
	}
}

// GroupWelcomeMarkup builds the keyboard shown when the bot is added to a group.
func GroupWelcomeMarkup() *gotdbot.ReplyMarkupInlineKeyboard {
	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{SetupGuideBtn},
			{url("Support Chat", config.SupportGroup), url("Updates", config.SupportChannel)},
		},
	}
}

// GuestReplyMarkup builds the keyboard for the personalized card sent back
// via AnswerGuestQuery when someone summons the bot with its @username (or
// a reply) in a chat it isn't a member of yet — Telegram's "Guest Bots"
// feature. Only URL buttons are used here (no callbacks): the bot has no
// ongoing presence in that chat until it's actually added, so a callback
// button on this card wouldn't have anything reliable to call back into.
func GuestReplyMarkup(username string) *gotdbot.ReplyMarkupInlineKeyboard {
	addMeBtn := urlPrimary("➕ Add Me to Your Group", fmt.Sprintf("https://t.me/%s?startgroup=true", username))

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{addMeBtn},
		},
	}
}

// GuideBackMarkup is shown on the setup guide screen: Add to Group (which
// the guide text explicitly tells the user to tap), then Back and Close.
func GuideBackMarkup(username string) *gotdbot.ReplyMarkupInlineKeyboard {
	addToGroupBtn := urlPrimary("➕ Add to Group", fmt.Sprintf("https://t.me/%s?startgroup=true", username))
	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{addToGroupBtn},
			{StartBackBtn, CloseBtn},
		},
	}
}
