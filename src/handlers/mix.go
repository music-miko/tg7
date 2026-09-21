/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/core/dl"
	"ashokshau/tgmusic/src/utils"
	"context"
	"fmt"
	"html"
	"time"

	td "github.com/AshokShau/gotdbot"
)

// mixLimit is how many tracks a single /mix adds at most.
const mixLimit = 10

// mixHandler handles the /mix command: queues a YouTube "Mix" (radio) built
// around a search query, a YouTube link, a replied-to message, or — when
// called with no arguments — whatever is playing right now.
func mixHandler(c *td.Client, m *td.Message) error {
	if !playMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId
	queueLen := cache.ChatCache.GetQueueLength(chatID)
	if queueLen >= MaxQueueLength {
		_, _ = m.ReplyText(c, fmt.Sprintf("Queue is full (max %d tracks). Use /end to clear it, or /remove to drop a specific track.", MaxQueueLength), nil)
		return td.EndGroups
	}

	isReply := m.ReplyToMessageID() != 0
	args := Args(m)
	url := getUrl(c, m, isReply)
	if isReply && args == "" && url == "" {
		if r, err := m.GetRepliedMessage(c); err == nil && r != nil {
			args = r.Text()
		}
	}
	input := coalesce(url, args)

	var seedTrackID string
	if input == "" {
		playing := cache.ChatCache.GetPlayingTrack(chatID)
		if playing == nil {
			_, _ = m.ReplyText(c, "<b>Usage:</b> /mix [song or YouTube link]\n\nOr run /mix while a song is playing to build a mix around it.", replyOpts)
			return td.EndGroups
		}
		// TrackID is only a YouTube video ID for YouTube tracks; for anything
		// else, seed the mix from the track's title instead.
		if playing.Platform == utils.YouTube && playing.TrackID != "" {
			seedTrackID = playing.TrackID
		} else {
			input = playing.Name
		}
	}

	updater, err := m.ReplyText(c, "🔍 Building your mix...", nil)
	if err != nil {
		c.Logger.Warn("failed to send message", "error", err)
		return td.EndGroups
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tracks, err := dl.GetYouTubeMix(ctx, input, seedTrackID, 0)
	if err != nil {
		_, err = updater.EditText(c, "Failed to build a mix: "+html.EscapeString(err.Error()), &td.EditTextMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Skip anything already queued/playing, and never overfill the queue.
	limit := min(mixLimit, MaxQueueLength-queueLen)
	fresh := make([]utils.MusicTrack, 0, limit)
	for _, t := range tracks {
		if cache.ChatCache.GetTrackIfExists(chatID, t.Id) != nil {
			continue
		}
		fresh = append(fresh, t)
		if len(fresh) >= limit {
			break
		}
	}

	if len(fresh) == 0 {
		msg := "No tracks found for that mix."
		if len(tracks) > 0 {
			msg = "Everything in this mix is already in the queue."
		}
		_, err = updater.EditText(c, msg, nil)
		return err
	}

	return handleMultipleTracks(c, m, updater, fresh, chatID, false, false)
}
