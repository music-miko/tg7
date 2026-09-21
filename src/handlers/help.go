/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"fmt"
	"html"
	"strings"

	"ashokshau/tgmusic/src/core"

	td "github.com/AshokShau/gotdbot"
)

// detailsBlock renders a collapsed-by-default <details>/<summary> section.
func detailsBlock(summary, body string) string {
	return fmt.Sprintf("<details><summary>%s</summary>\n%s</details>", summary, body)
}

// cmdTable renders a Command | Description table from pairs of
// {command, description}. Descriptions are already trusted, static text.
func cmdTable(rows ...[2]string) string {
	var sb strings.Builder
	sb.WriteString("<table bordered striped>")
	sb.WriteString("<tr><th>Command</th><th>Description</th></tr>")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("<tr><td align=\"left\"><code>%s</code></td><td align=\"left\">%s</td></tr>", r[0], r[1]))
	}
	sb.WriteString("</table>")
	return sb.String()
}

func getHelpCategories() map[string]struct {
	Title   string
	Content string
	Markup  *td.ReplyMarkupInlineKeyboard
} {
	return map[string]struct {
		Title   string
		Content string
		Markup  *td.ReplyMarkupInlineKeyboard
	}{
		"help_user": {
			Title: "User Commands",
			Content: detailsBlock("Playback", cmdTable(
				[2]string{"/play [song]", "Searches and plays a track in the group's voice chat. Accepts a search query or a direct link (YouTube, Spotify, Apple Music, SoundCloud, Deezer, JioSaavn)."},
				[2]string{"/vplay [song]", "Same as /play, but streams video instead of audio."},
			)) + "\n" +
				"<i>Admins have two more ways to play — see Autoplay and Admin Commands below.</i>\n" +
				detailsBlock("Utilities", cmdTable(
					[2]string{"/start", "Shows the welcome message and setup options."},
					[2]string{"/privacy", "Shows the bot's privacy policy."},
					[2]string{"/queue", "Shows what's currently playing and what's coming up next."},
				)),
			Markup: core.BackHelpMenuKeyboard(),
		},
		"help_admin": {
			Title: "Admin Commands",
			Content: "<i>Unless Admin Mode is turned off in /settings, these require being a group admin or an authorized user.</i>\n\n" +
				detailsBlock("Playback Controls", cmdTable(
					[2]string{"/skip", "Skips the current track and plays the next one in queue."},
					[2]string{"/pause", "Pauses playback without clearing the queue."},
					[2]string{"/resume", "Resumes playback after a pause."},
					[2]string{"/seek [seconds]", "Jumps to a specific position in the current track."},
					[2]string{"/mute", "Mutes the bot's audio in the voice chat without stopping playback."},
					[2]string{"/unmute", "Unmutes the bot's audio after /mute."},
				)) + "\n" +
				detailsBlock("Force Play", ""+
					"<p>Normally, <code>/play</code> and <code>/vplay</code> add a track to the <b>end</b> of the queue — it waits its turn. "+
					"<code>/fplay</code> and <code>/fvplay</code> skip that wait: they insert the track right after whatever's currently "+
					"playing and, if something was already streaming, switch to it immediately instead of waiting for the queue to get there. "+
					"Nothing else in the queue is removed — everyone else's tracks just get pushed back one spot.</p>"+
					cmdTable(
						[2]string{"/fplay [song]", "Force-plays a track (audio) to the front of the queue."},
						[2]string{"/fvplay [song]", "Force-plays a video to the front of the queue — the /vplay of force-play."},
					),
				) + "\n" +
				detailsBlock("Queue Management", cmdTable(
					[2]string{"/remove [position]", "Removes a specific track from the queue by its position number."},
					[2]string{"/loop [0-10]", "Repeats the current track 0-10 times; 0 turns looping off."},
				)) + "\n" +
				detailsBlock("Access Control", cmdTable(
					[2]string{"/auth [reply]", "Authorizes a user to use admin commands even if they aren't a group admin. Reply to their message."},
					[2]string{"/unauth [reply]", "Removes a previously authorized user's access."},
					[2]string{"/authlist", "Lists everyone currently authorized in this chat."},
				)),
			Markup: core.BackHelpMenuKeyboard(),
		},
		"help_autoplay": {
			Title: "Autoplay",
			Content: "<p>Automatically keeps the music going once the queue runs dry, instead of the bot falling silent.</p>\n\n" +
				detailsBlock("How it works", ""+
					"<p>When the last track in the queue finishes, the bot looks up tracks related to it on YouTube — the same pool "+
					"YouTube itself draws from for its own \"Mix\" radio playlists — and queues up a random pick from there. "+
					"This repeats for as long as autoplay stays on, so the voice chat never just goes quiet.</p>",
				) + "\n" +
				cmdTable([2]string{"/autoplay", "Toggles autoplay on or off for this chat, with a button to flip it without retyping the command."}) + "\n" +
				"<i>Requires admin or an authorized user. Autoplay turns itself back off whenever /stop or /end fully clears the queue.</i>",
			Markup: core.BackHelpMenuKeyboard(),
		},
		"help_devs": {
			Title: "Developer Commands",
			Content: "<i>These are available only to the bot's developers and won't do anything if you're not one.</i>\n\n" +
				detailsBlock("Diagnostics", cmdTable(
					[2]string{"/stats", "Shows runtime statistics: CPU, memory, storage, uptime, and database counts."},
					[2]string{"/av", "Lists every chat with an active voice/video chat, its queue size, and what's playing."},
					[2]string{"/yt", "Shows YouTube downloader/API usage statistics."},
					[2]string{"/gs", "Shows aggregate stats across every group the bot is in."},
				)) + "\n" +
				detailsBlock("Assistant Management", cmdTable(
					[2]string{"/clearass", "Disconnects and clears every assistant's chat assignments."},
					[2]string{"/leaveAll", "Makes every assistant leave every chat it's currently in."},
					[2]string{"/logger", "Shows or toggles the bot's playback logging."},
				)) + "\n" +
				detailsBlock("Backups", cmdTable(
					[2]string{"/backup", "Takes an on-demand full database backup and sends it as a .zip here."},
					[2]string{"/restore", "Reply to a backup .zip with this to restore the database from it. A daily automatic backup is also sent to the configured backup chat."},
				)),
			Markup: core.BackHelpMenuKeyboard(),
		},
		"help_owner": {
			Title: "Owner Commands",
			Content: detailsBlock("Settings", cmdTable(
				[2]string{"/settings", "Opens this chat's settings: admin mode, play mode, command auto-delete, and language."},
			)),
			Markup: core.BackHelpMenuKeyboard(),
		},
		"help_playlist": {
			Title: "Playlist Commands",
			Content: detailsBlock("Management", cmdTable(
				[2]string{"/createplaylist [name]", "Creates a new personal playlist you can add tracks to."},
				[2]string{"/deleteplaylist [id]", "Permanently deletes one of your playlists."},
				[2]string{"/addtoplaylist [id] [url]", "Adds a track to an existing playlist by its ID."},
				[2]string{"/removefromplaylist [id] [url]", "Removes a track from a playlist by its ID."},
				[2]string{"/playlistinfo [id]", "Shows a playlist's name, owner, and full track list."},
				[2]string{"/myplaylists", "Lists all playlists you own."},
			)),
			Markup: core.BackHelpMenuKeyboard(),
		},
	}
}

// helpCallbackHandler handles the /help menu's buttons. It's only ever
// reached from the private-chat /start screen (groups don't wire up these
// buttons).
//
// /start, the category menu, each category page, and "Home" are all Rich
// Messages — the welcome image lives in-message via <img> rather than as a
// separate photo caption — so every transition here is a plain in-place
// edit. Nothing is ever deleted and resent.
func helpCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	data := cb.DataString()

	user, err := c.GetUser(cb.SenderUserId)
	if err != nil {
		user = &td.User{FirstName: "User", Id: cb.SenderUserId}
	}

	helpCategories := getHelpCategories()

	if strings.Contains(data, "help_all") {
		_ = cb.Answer(c, 0, false, "Opening help menu...", "")
		response := fmt.Sprintf(
			"%s\nHello %s, pick a category below to see what I can do.\n\n<b>Supported platforms:</b> YouTube, Spotify, Apple Music, SoundCloud, Deezer, JioSaavn and more.",
			headingBlock(3, fmt.Sprintf("📖 %s — Help Menu", html.EscapeString(c.Me.FirstName))),
			html.EscapeString(user.FirstName),
		)
		_, err := editRichByID(c, cb.ChatId, cb.MessageId, response, core.HelpMenuKeyboard())
		return err
	}

	if strings.Contains(data, "help_back") {
		_ = cb.Answer(c, 0, false, "Returning to main menu...", "")
		response := privateWelcomeText(user.FirstName, c.Me.FirstName)
		markup := core.PrivateStartMarkup(c.Me.Usernames.EditableUsername)
		_, err := editRichByID(c, cb.ChatId, cb.MessageId, response, markup)
		return err
	}

	if category, ok := helpCategories[data]; ok {
		_ = cb.Answer(c, 0, false, category.Title, "")
		response := fmt.Sprintf("%s\n\n%s\n\n<i>Use the buttons below to go back.</i>", headingBlock(2, category.Title), category.Content)
		_, err := editRichByID(c, cb.ChatId, cb.MessageId, response, category.Markup)
		return err
	}

	_ = cb.Answer(c, 0, true, "Unknown help category.", "")
	return nil
}
