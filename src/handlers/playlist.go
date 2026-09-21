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
	"strconv"
	"strings"

	"ashokshau/tgmusic/src/core/db"
	"ashokshau/tgmusic/src/core/dl"
	"ashokshau/tgmusic/src/utils"

	td "github.com/AshokShau/gotdbot"
)

func createPlaylistHandler(c *td.Client, m *td.Message) error {

	userID := m.SenderID()

	args := Args(m)
	if args == "" {
		_, err := m.ReplyText(c, "<b>Usage:</b> /createplaylist [playlist name]", replyOpts)
		return err
	}

	userPlaylists, err := db.Instance.GetUserPlaylists(userID)
	if err != nil {
		_, err = m.ReplyText(c, "Unable to fetch your playlists. Please try again later.", nil)
		return err
	}

	if len(userPlaylists) >= 10 {
		_, _ = m.ReplyText(c, "You have reached the maximum limit of 10 playlists.", nil)
		return td.EndGroups
	}

	if len([]rune(args)) > 40 {
		args = string([]rune(args)[:40])
	}

	playlistID, err := db.Instance.CreatePlaylist(args, userID)
	if err != nil {
		_, err = m.ReplyText(c, fmt.Sprintf("Failed to create playlist: %s", err.Error()), nil)
		return err
	}

	_, err = m.ReplyText(
		c,
		fmt.Sprintf(
			"Playlist <b>%s</b> has been created successfully.\nID: <code>%s</code>",
			html.EscapeString(args),
			html.EscapeString(playlistID),
		),
		replyOpts,
	)

	return td.EndGroups
}

func deletePlaylistHandler(c *td.Client, m *td.Message) error {

	userID := m.SenderID()

	args := Args(m)
	if args == "" {
		_, err := m.ReplyText(
			c,
			"<b>Usage:</b> /deleteplaylist [playlist id]",
			&td.SendTextMessageOpts{ParseMode: "HTML"},
		)
		return err
	}

	playlist, err := db.Instance.GetPlaylist(args)
	if err != nil {
		_, err := m.ReplyText(
			c,
			"The specified playlist could not be found. Please check the playlist ID.",
			nil,
		)
		return err
	}

	if playlist.UserID != userID {
		_, err := m.ReplyText(
			c,
			"You can only delete playlists that you created.",
			nil,
		)
		return err
	}

	err = db.Instance.DeletePlaylist(args, userID)
	if err != nil {
		_, err := m.ReplyText(
			c,
			fmt.Sprintf("Failed to delete the playlist: %s", err.Error()),
			nil,
		)
		return err
	}

	_, err = m.ReplyText(
		c,
		fmt.Sprintf("Playlist <b>%s</b> has been deleted successfully.", html.EscapeString(playlist.Name)),
		&td.SendTextMessageOpts{ParseMode: "HTML"},
	)

	return err
}
func addToPlaylistHandler(c *td.Client, m *td.Message) error {

	userID := m.SenderID()

	args := strings.SplitN(Args(m), " ", 2)
	if len(args) != 2 {
		_, err := m.ReplyText(
			c,
			"<b>Usage:</b> /addtoplaylist [playlist id] [song url]",
			&td.SendTextMessageOpts{ParseMode: "HTML"},
		)
		return err
	}

	playlistID := args[0]
	songURL := args[1]

	playlist, err := db.Instance.GetPlaylist(playlistID)
	if err != nil {
		_, err := m.ReplyText(
			c,
			"The specified playlist could not be found. Please verify the playlist ID.",
			nil,
		)
		return err
	}

	if playlist.UserID != userID {
		_, err := m.ReplyText(
			c,
			"You can only modify playlists that you created.",
			nil,
		)
		return err
	}

	wrapper := dl.NewDownloaderWrapper(songURL)
	if !wrapper.IsValid() {
		_, err := m.ReplyText(
			c,
			"The provided URL is invalid or the platform is not supported.",
			nil,
		)
		return err
	}

	trackInfo, err := wrapper.GetInfo()
	if err != nil {
		_, err := m.ReplyText(
			c,
			fmt.Sprintf("Unable to retrieve track information: %s", err.Error()),
			nil,
		)
		return err
	}

	if trackInfo.Results == nil || len(trackInfo.Results) == 0 {
		_, err := m.ReplyText(
			c,
			"No playable tracks were found for the provided link.",
			nil,
		)
		return err
	}

	song := db.Song{
		URL:      trackInfo.Results[0].Url,
		Name:     trackInfo.Results[0].Title,
		TrackID:  trackInfo.Results[0].Id,
		Duration: trackInfo.Results[0].Duration,
		Platform: trackInfo.Results[0].Platform,
	}

	err = db.Instance.AddSongToPlaylist(playlistID, song)
	if err != nil {
		_, err := m.ReplyText(
			c,
			fmt.Sprintf("Failed to add the track to the playlist: %s", err.Error()),
			nil,
		)
		return err
	}

	_, err = m.ReplyText(
		c,
		fmt.Sprintf(
			"Track <b>%s</b> has been added to playlist <b>%s</b>.",
			html.EscapeString(song.Name),
			html.EscapeString(playlist.Name),
		),
		replyOpts,
	)

	return err
}

func removeFromPlaylistHandler(c *td.Client, m *td.Message) error {

	userID := m.SenderID()

	args := strings.SplitN(Args(m), " ", 2)
	if len(args) != 2 {
		_, err := m.ReplyText(
			c,
			"<b>Usage:</b> /removefromplaylist [playlist id] [song number or url]",
			&td.SendTextMessageOpts{ParseMode: "HTML"},
		)
		return err
	}

	playlistID := args[0]
	songIdentifier := args[1]

	playlist, err := db.Instance.GetPlaylist(playlistID)
	if err != nil {
		_, err = m.ReplyText(c, "Playlist not found.", nil)
		return err
	}

	if playlist.UserID != userID {
		_, err = m.ReplyText(c, "You do not own this playlist.", nil)
		return err
	}

	songIndex, err := strconv.Atoi(songIdentifier)
	var trackID string

	if err == nil {
		if songIndex < 1 || songIndex > len(playlist.Songs) {
			_, err := m.ReplyText(c, "Invalid song number.", nil)
			return err
		}
		trackID = playlist.Songs[songIndex-1].TrackID
	} else {
		for _, song := range playlist.Songs {
			if song.URL == songIdentifier || song.TrackID == songIdentifier {
				trackID = song.TrackID
				break
			}
		}
	}

	if trackID == "" {
		_, err = m.ReplyText(c, "Song not found in playlist.", nil)
		return err
	}

	err = db.Instance.RemoveSongFromPlaylist(playlistID, trackID)
	if err != nil {
		_, err = m.ReplyText(c, fmt.Sprintf("Error removing song: %s", err.Error()), nil)
		return err
	}

	_, err = m.ReplyText(c, fmt.Sprintf("Song removed from playlist '%s'.", playlist.Name), nil)
	return err
}

func playlistInfoHandler(c *td.Client, m *td.Message) error {

	args := Args(m)
	if args == "" {
		_, err := replyRich(c, m, "<b>Usage:</b> /playlistinfo [playlist id]", nil)
		return err
	}

	playlist, err := db.Instance.GetPlaylist(args)
	if err != nil {
		_, err = m.ReplyText(c, "Playlist not found.", nil)
		return err
	}

	owner, err := c.GetUser(playlist.UserID)
	if err != nil {
		return td.EndGroups
	}

	var b strings.Builder
	b.WriteString(headingBlock(4, fmt.Sprintf("Playlist: %s", html.EscapeString(playlist.Name))))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("• <b>Owner:</b> %s\n", html.EscapeString(owner.FirstName)))
	b.WriteString(fmt.Sprintf("• <b>ID:</b> <code>%s</code>\n", html.EscapeString(playlist.ID)))
	b.WriteString(fmt.Sprintf("• <b>Songs:</b> %d\n", len(playlist.Songs)))

	if len(playlist.Songs) == 0 {
		b.WriteString("\n<i>This playlist has no songs yet.</i>")
	} else {
		b.WriteString("\n<table bordered striped>")
		b.WriteString("<tr><th align=\"center\">#</th><th>Title</th><th align=\"center\">Duration</th><th>Platform</th></tr>")

		shown := 0
		for i, song := range playlist.Songs {
			if i >= 30 {
				break
			}
			shown++
			b.WriteString(fmt.Sprintf(
				"<tr><td align=\"center\">%d</td><td align=\"left\"><a href=\"%s\">%s</a></td><td align=\"center\">%s</td><td align=\"center\">%s</td></tr>",
				i+1,
				html.EscapeString(song.URL),
				html.EscapeString(truncate(song.Name, 40)),
				utils.SecToMin(song.Duration),
				html.EscapeString(song.Platform),
			))
		}
		b.WriteString("</table>")

		if len(playlist.Songs) > shown {
			b.WriteString(fmt.Sprintf("\n<i>...and %d more tracks</i>", len(playlist.Songs)-shown))
		}
	}

	_, err = replyRich(c, m, b.String(), nil)
	return td.EndGroups
}

func myPlaylistsHandler(c *td.Client, m *td.Message) error {

	userID := m.SenderID()

	playlists, err := db.Instance.GetUserPlaylists(userID)
	if err != nil {
		_, err := m.ReplyText(c, fmt.Sprintf("Error fetching playlists: %s", err.Error()), nil)
		return err
	}

	if len(playlists) == 0 {
		_, err := m.ReplyText(c, "You do not have any playlists.", nil)
		return err
	}

	var b strings.Builder
	b.WriteString(headingBlock(4, "My Playlists"))
	b.WriteString("\n\n<table bordered striped>")
	b.WriteString("<tr><th>Name</th><th align=\"center\">ID</th><th align=\"center\">Songs</th></tr>")

	for _, playlist := range playlists {
		b.WriteString(fmt.Sprintf(
			"<tr><td align=\"left\">%s</td><td align=\"center\"><code>%s</code></td><td align=\"center\">%d</td></tr>",
			html.EscapeString(truncate(playlist.Name, 30)),
			html.EscapeString(playlist.ID),
			len(playlist.Songs),
		))
	}
	b.WriteString("</table>")
	b.WriteString(fmt.Sprintf("\n<i>Use /playlistinfo &lt;id&gt; to view a playlist's songs.</i>"))

	_, err = replyRich(c, m, b.String(), nil)
	return err
}
