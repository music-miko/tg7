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

// cbStyled is cb() with an explicit button Style (Bot API 10.3's colored
// bot buttons - default/primary/success/danger/link).
func cbStyled(text, data string, style gotdbot.ButtonStyle) gotdbot.InlineKeyboardButton {
	btn := cb(text, data)
	btn.Style = style
	return btn
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

var CloseBtn = cbStyled("Close", "vcplay_close", gotdbot.ButtonStyleDanger{})
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
	addToPlaylistBtn := cbStyled("➕", "play_add_to_list", gotdbot.ButtonStylePrimary{})

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

// QueueAddedMarkup is shown on "Added to queue" notifications instead of
// the full playback controls (skip/pause/etc. act on the *currently
// playing* track, which this message isn't — showing them here was
// confusing and duplicated the Now Streaming card's own controls). Every
// queue-add is also a high-frequency moment where non-members watching the
// group can see the bot working, so it doubles as a quiet growth CTA.
func QueueAddedMarkup(username string) *gotdbot.ReplyMarkupInlineKeyboard {
	addMeBtn := urlPrimary("➕Add me", fmt.Sprintf("https://t.me/%s?startgroup=true", username))

	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{addMeBtn, CloseBtn},
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

// GroupWelcomeMarkup builds the keyboard shown when the bot is added to a
// group. Help/Close used to sit here; swapped for Updates/Support as real
// buttons in one row (the same simple pattern as the /autoplay and
// /settings panels) since those are the two links people actually want
// from this screen.
func GroupWelcomeMarkup() *gotdbot.ReplyMarkupInlineKeyboard {
	return &gotdbot.ReplyMarkupInlineKeyboard{
		Rows: [][]gotdbot.InlineKeyboardButton{
			{url("Updates", config.SupportChannel), url("Support", config.SupportGroup)},
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
