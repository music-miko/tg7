/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/core/cache"
)

const (
	// cleanupInterval is how often the media cache is swept.
	cleanupInterval = 12 * time.Hour
	// cleanupMaxAge is how old a cached file must be before it's removed.
	// Kept equal to cleanupInterval so a file only ever survives one sweep
	// past the run it was created in.
	cleanupMaxAge = 12 * time.Hour
)

// mediaCacheDirs maps the TDLib file-type subdirectories we clean to the
// file extensions (lowercase, with dot) we're allowed to remove from each.
// TDLib sorts files it sends/downloads into these subdirectories under its
// database directory by type — videos (.mp4) land in "videos", while other
// media the bot sends (webm clips, m4a audio, etc.) land in "music" or
// "documents" depending on how Telegram classified the attachment.
var mediaCacheDirs = map[string][]string{
	"videos":    {".mp4"},
	"music":     {".mp3", ".m4a", ".webm", ".ogg", ".opus", ".flac", ".wav", ".aac"},
	"documents": {".mp3", ".m4a", ".webm", ".ogg", ".opus", ".flac", ".wav", ".aac"},
}

// StartDownloadsCleanup runs forever in the background, sweeping stale
// cached media out of tdDatabaseDir's videos/music/documents subdirectories
// every cleanupInterval, plus the loose scratch files this bot writes
// directly into config.DownloadsDir while downloading. It runs one pass
// immediately on startup as well.
//
// tdDatabaseDir should be the same DatabaseDirectory the gotdbot client was
// configured with in main.go (TDLib's own storage root) — that's where
// videos/, music/, and documents/ actually live, not necessarily
// config.DownloadsDir, even though the two default to the same path.
func StartDownloadsCleanup(ctx context.Context, tdDatabaseDir string) {
	go func() {
		slog.Info("[Cleanup] media cleanup task starting",
			"tdDatabaseDir", tdDatabaseDir,
			"downloadsDir", config.DownloadsDir,
			"interval", cleanupInterval,
			"maxAge", cleanupMaxAge,
		)

		runCleanup(tdDatabaseDir)

		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("[Cleanup] media cleanup task stopped")
				return
			case <-ticker.C:
				runCleanup(tdDatabaseDir)
			}
		}
	}()
}

func runCleanup(tdDatabaseDir string) {
	inUse := activeFilePaths()
	cleanupMediaCache(tdDatabaseDir, inUse)
	cleanupDownloads(inUse)
}

// activeFilePaths returns the set of local file paths currently referenced
// by any chat's queue — the track that's playing right now, everything
// queued behind it, and any prefetched-but-not-yet-playing next track.
// downloadAndPrepareSong/PlayMedia stream directly from CachedTrack.FilePath
// for as long as a track stays in a chat's queue, which can outlast a
// single cleanup interval (long queues, loop mode, quiet chats), so these
// paths must never be swept regardless of file age.
func activeFilePaths() map[string]struct{} {
	inUse := make(map[string]struct{})

	for _, chatID := range cache.ChatCache.GetActiveChats() {
		for _, track := range cache.ChatCache.GetQueue(chatID) {
			if track == nil || track.FilePath == "" {
				continue
			}
			inUse[filepath.Clean(track.FilePath)] = struct{}{}
		}
	}

	return inUse
}

// cleanupMediaCache walks each subdirectory in mediaCacheDirs under
// tdDatabaseDir and removes files whose extension is allow-listed for that
// subdirectory and that haven't been modified in the last cleanupMaxAge.
// Only regular files matching an allow-listed extension are ever touched;
// everything else (including any nested directories TDLib creates) is left
// alone.
func cleanupMediaCache(tdDatabaseDir string, inUse map[string]struct{}) {
	if tdDatabaseDir == "" {
		return
	}

	cutoff := time.Now().Add(-cleanupMaxAge)

	for subdir, exts := range mediaCacheDirs {
		dir := filepath.Join(tdDatabaseDir, subdir)

		var removed, skippedInUse, failed int
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries rather than aborting the whole walk
			}
			if d.IsDir() {
				return nil
			}

			if !hasAllowedExt(d.Name(), exts) {
				return nil
			}

			if _, active := inUse[filepath.Clean(path)]; active {
				skippedInUse++
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.ModTime().After(cutoff) {
				return nil
			}

			if err := os.Remove(path); err != nil {
				failed++
				slog.Warn("[Cleanup] failed to remove cached media file", "file", path, "error", err)
				return nil
			}
			removed++
			return nil
		})

		if err != nil && !os.IsNotExist(err) {
			slog.Error("[Cleanup] failed to walk media cache directory", "dir", dir, "error", err)
			continue
		}

		if removed > 0 || failed > 0 || skippedInUse > 0 {
			slog.Info("[Cleanup] media cache directory swept", "dir", dir, "removed", removed, "skippedInUse", skippedInUse, "failed", failed)
		}
	}
}

// hasAllowedExt reports whether name's extension (case-insensitive) is in exts.
func hasAllowedExt(name string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// cleanupDownloads removes regular files directly inside config.DownloadsDir
// that haven't been modified in the last cleanupMaxAge and aren't currently
// referenced by any chat's queue (inUse) — the .tmp/.part/.ogg/etc scratch
// files this bot's own downloader writes while fetching a track, plus
// finished downloads once nothing is playing/queuing them anymore.
// Subdirectories and dotfiles are left untouched, so this is safe to run
// even when DownloadsDir happens to point at the same root as TDLib's own
// storage.
func cleanupDownloads(inUse map[string]struct{}) {
	dir := config.DownloadsDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("[Cleanup] failed to read downloads directory", "dir", dir, "error", err)
		}
		return
	}

	cutoff := time.Now().Add(-cleanupMaxAge)
	var removed, skippedInUse, failed int

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) > 0 && name[0] == '.' {
			continue
		}

		path := filepath.Join(dir, name)
		if _, active := inUse[filepath.Clean(path)]; active {
			skippedInUse++
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(cutoff) {
			continue
		}

		if err := os.Remove(path); err != nil {
			failed++
			slog.Warn("[Cleanup] failed to remove stale download", "file", path, "error", err)
			continue
		}
		removed++
	}

	if removed > 0 || failed > 0 || skippedInUse > 0 {
		slog.Info("[Cleanup] downloads directory swept", "removed", removed, "skippedInUse", skippedInUse, "failed", failed)
	}
}
