/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/core/dl"
	"ashokshau/tgmusic/src/utils"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os/exec"
	"strconv"
	"strings"
	"time"

	td "github.com/AshokShau/gotdbot"
	"github.com/amarnathcjd/gogram/telegram"
)

// downloadAndPrepareSong handles the download and preparation of a song for playback.
// It returns an error if the download or preparation fails.
func (c *TelegramCalls) downloadAndPrepareSong(bot *td.Client, song *utils.CachedTrack, reply *td.Message) error {
	if song.FilePath != "" {
		return nil
	}

	dlPath, err := dl.DownloadCachedTrack(song, bot)
	song.FilePath = dlPath
	if err != nil || song.FilePath == "" {
		_, _ = reply.EditText(bot, "⚠️ Download failed. Skipping track...", nil)
		return err
	}

	return nil
}

// PlayNext plays the next song in the queue, handles looping, and notifies the chat when the queue is finished.
func (c *TelegramCalls) PlayNext(bot *td.Client, chatID int64) error {
	loop := cache.ChatCache.GetLoopCount(chatID)
	if loop > 0 {
		cache.ChatCache.SetLoopCount(chatID, loop-1)
		if currentsSong := cache.ChatCache.GetPlayingTrack(chatID); currentsSong != nil {
			return c.playSong(bot, chatID, currentsSong)
		}
	}

	if nextSong := cache.ChatCache.GetUpcomingTrack(chatID); nextSong != nil {
		cache.ChatCache.RemoveCurrentSong(chatID)
		return c.playSong(bot, chatID, nextSong)
	}

	cache.ChatCache.RemoveCurrentSong(chatID)

	lastSong := cache.ChatCache.GetLastYouTubeTrack(chatID)
	if lastSong != nil && cache.ChatCache.GetAutoplay(chatID) {
		return c.handleAutoplay(bot, chatID, lastSong)
	}

	return c.handleNoSong(bot, chatID)
}

// handleAutoplay is called when the queue runs dry in a chat with autoplay
// enabled. It pulls Telegram's own "Mix" recommendations for the last
// YouTube track that played (the same playlist backing the "RD..." radio
// mixes on youtube.com) and queues up a random pick from it, so the music
// keeps going without anyone having to run /play again.
func (c *TelegramCalls) handleAutoplay(bot *td.Client, chatID int64, lastSong *utils.CachedTrack) error {
	playlistID := "RD" + lastSong.TrackID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tracks, err := dl.GetYouTubeMixPlaylist(ctx, playlistID)
	if err != nil || len(tracks.Results) == 0 {
		return c.handleNoSong(bot, chatID)
	}

	var candidates []utils.MusicTrack
	for _, t := range tracks.Results {
		if t.Id != lastSong.TrackID {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		return c.handleNoSong(bot, chatID)
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	var nextTrack utils.MusicTrack
	if err != nil {
		nextTrack = candidates[0]
	} else {
		nextTrack = candidates[n.Int64()]
	}

	saveCache := &utils.CachedTrack{
		URL: nextTrack.Url, Name: nextTrack.Title, User: "Autoplay",
		Thumbnail: nextTrack.Thumbnail, TrackID: nextTrack.Id, Duration: nextTrack.Duration,
		Channel: nextTrack.Channel, Views: nextTrack.Views, IsVideo: lastSong.IsVideo, Platform: utils.YouTube,
	}

	cache.ChatCache.AddSong(chatID, saveCache)
	return c.playSong(bot, chatID, saveCache)
}

// handleNoSong manages the situation where there are no more songs in the queue by stopping the playback
// and sending a notification to the chat.
func (c *TelegramCalls) handleNoSong(bot *td.Client, chatID int64) error {
	_ = c.Stop(chatID, false)
	_, _ = bot.SendTextMessage(chatID, "🎵 Queue finished. Add more songs with /play.", nil)
	return nil
}

// floodWaitMaxAutoRetry is the longest FLOOD_WAIT we sleep through automatically.
// Telegram's short/medium floods for MTProto calls (join/leave channel, get
// dialogs, resolve peer, etc.) are almost always well under a couple of
// minutes, so waiting them out and letting gogram retry is far better than
// giving up on the underlying operation immediately. Anything longer than
// this is treated as abnormal and given up on, so a single stuck client
// can't block other work indefinitely.
const floodWaitMaxAutoRetry = 90 * time.Second

// handleFlood manages flood wait errors by pausing execution for the wait
// duration, up to floodWaitMaxAutoRetry. Returning true tells gogram to
// retry the request that triggered the flood wait; returning false gives up
// and propagates the error to the caller.
func handleFlood(err error) bool {
	wait := telegram.GetFloodWait(err)
	if wait <= 0 {
		return false
	}

	waitDuration := time.Duration(wait) * time.Second
	if waitDuration > floodWaitMaxAutoRetry {
		logger.Warn("Flood wait too long, giving up", "seconds", wait)
		return false
	}

	logger.Warn("Flood wait detected, sleeping", "seconds", wait)
	time.Sleep(waitDuration + time.Second)
	return true
}

func getVideoDimensions(filePath string) (int, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", filePath)
	out, err := cmd.Output()
	if err != nil {
		logger.Warn("[getVideoDimensions] Failed to get video dimensions (%s): %v", filePath, err)
		return 0, 0
	}
	dimensions := strings.Split(strings.TrimSpace(string(out)), "x")
	if len(dimensions) != 2 {
		logger.Warn("[getVideoDimensions] Invalid video dimensions(%s): %s", filePath, string(out))
		return 0, 0
	}

	width, _ := strconv.Atoi(dimensions[0])
	height, _ := strconv.Atoi(dimensions[1])
	return width, height
}

// UpdateMembership updates the membership status of a user in a specific chat.
func (c *TelegramCalls) UpdateMembership(chatId, userId int64, status td.ChatMemberStatus) {
	cacheKey := fmt.Sprintf("%d:%d", chatId, userId)
	if c.statusCache != nil {
		c.statusCache.Set(cacheKey, status)
		logger.Info("[UpdateMembership] The cache has been updated: chat= user= status=", "chat_id", chatId, "user_id", userId, "arg3", status)
	}
}

// UpdateInviteLink updates the invite link for a specific chat.
func (c *TelegramCalls) UpdateInviteLink(chatId int64, link string) {
	cacheKey := strconv.FormatInt(chatId, 10)
	if link == "" {
		c.inviteCache.Delete(cacheKey)
		return
	}
	c.inviteCache.Set(cacheKey, link)
}
