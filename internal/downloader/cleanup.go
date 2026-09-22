/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package downloader

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ashokshau/tgmusic/internal/cache"
	"ashokshau/tgmusic/internal/config"

	"github.com/shirou/gopsutil/v3/disk"
)

const (
	// cleanupInterval is how often the media cache is swept on the normal,
	// age-based schedule.
	cleanupInterval = 12 * time.Hour
	// cleanupMaxAge is how old a cached file must be before the routine
	// (non-urgent) sweep removes it. Kept equal to cleanupInterval so a
	// file only ever survives one sweep past the run it was created in.
	cleanupMaxAge = 12 * time.Hour

	// diskCheckInterval is how often disk usage is polled to decide
	// whether an urgent, immediate cleanup is needed.
	diskCheckInterval = 5 * time.Minute
)

// mediaCacheDirs maps the TDLib file-type subdirectories we clean to the
// file extensions (lowercase, with dot) we're allowed to remove from each.
// TDLib sorts files it sends/downloads into these subdirectories under its
// database directory by type - videos (.mp4) land in "videos", while other
// media the bot sends (webm clips, m4a audio, etc.) land in "music" or
// "documents" depending on how Telegram classified the attachment.
var mediaCacheDirs = map[string][]string{
	"videos":    {".mp4"},
	"music":     {".mp3", ".m4a", ".webm", ".ogg", ".opus", ".flac", ".wav", ".aac"},
	"documents": {".mp3", ".m4a", ".webm", ".ogg", ".opus", ".flac", ".wav", ".aac"},
}

// StartDownloadsCleanup runs forever in the background, keeping the media
// cache under control two ways:
//
//  1. A routine sweep every cleanupInterval that removes files older than
//     cleanupMaxAge from tdDatabaseDir's videos/music/documents
//     subdirectories, plus loose scratch files in config.DownloadsDir.
//  2. A disk-usage watchdog polled every diskCheckInterval: as soon as the
//     filesystem holding config.DownloadsDir is at or above
//     config.DiskCleanupThreshold percent full, a sweep runs immediately
//     (ignoring cleanupMaxAge - every unused file is fair game), instead of
//     waiting for the next scheduled run. Once usage drops back below the
//     threshold, cleanup goes back to waiting on the normal schedule.
//
// It runs one routine pass immediately on startup as well.
//
// tdDatabaseDir should be the same DatabaseDirectory the gotdbot client was
// configured with in main.go (TDLib's own storage root) - that's where
// videos/, music/, and documents/ actually live, not necessarily
// config.DownloadsDir, even though the two default to the same path.
func StartDownloadsCleanup(ctx context.Context, tdDatabaseDir string) {
	go func() {
		slog.Info("[Cleanup] media cleanup task starting",
			"tdDatabaseDir", tdDatabaseDir,
			"downloadsDir", config.DownloadsDir,
			"interval", cleanupInterval.String(),
			"maxAge", cleanupMaxAge.String(),
			"diskThreshold", config.DiskCleanupThreshold,
		)

		runCleanup(tdDatabaseDir, false)

		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		diskTicker := time.NewTicker(diskCheckInterval)
		defer diskTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("[Cleanup] media cleanup task stopped")
				return
			case <-ticker.C:
				runCleanup(tdDatabaseDir, false)
			case <-diskTicker.C:
				checkDiskAndCleanupIfNeeded(tdDatabaseDir)
			}
		}
	}()
}

// checkDiskAndCleanupIfNeeded reads current disk usage for the filesystem
// backing config.DownloadsDir and, if it's at or above
// config.DiskCleanupThreshold percent, runs an urgent cleanup pass right
// away rather than waiting for the next scheduled sweep.
func checkDiskAndCleanupIfNeeded(tdDatabaseDir string) {
	usage, err := diskUsagePercent(config.DownloadsDir)
	if err != nil {
		slog.Warn("[Cleanup] failed to read disk usage", "path", config.DownloadsDir, "error", err)
		return
	}

	if usage < config.DiskCleanupThreshold {
		return
	}

	slog.Warn("[Cleanup] disk usage above threshold, running an immediate cleanup",
		"used_percent", usage,
		"threshold", config.DiskCleanupThreshold,
	)
	runCleanup(tdDatabaseDir, true)
}

// diskUsagePercent returns the percentage of disk space used on the
// filesystem that contains path.
func diskUsagePercent(path string) (float64, error) {
	stat, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	return stat.UsedPercent, nil
}

// runCleanup sweeps the media cache and downloads directory once. When
// urgent is true (the disk-usage watchdog fired), every unused file is
// removed regardless of age; otherwise only files older than cleanupMaxAge
// are touched.
func runCleanup(tdDatabaseDir string, urgent bool) {
	inUse := activeFilePaths()
	cleanupMediaCache(tdDatabaseDir, inUse, urgent)
	cleanupDownloads(inUse, urgent)
}

// activeFilePaths returns the set of local file paths currently referenced
// by any chat's queue - the track that's playing right now and everything
// queued behind it. downloadAndPrepareSong/PlayMedia stream directly from
// a queued track's FilePath for as long as it stays in a chat's queue,
// which can outlast a single cleanup interval (long queues, loop mode,
// quiet chats), so these paths must never be swept regardless of age or
// how urgently disk space is needed.
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
// subdirectory. When urgent is false, only files untouched for
// cleanupMaxAge are removed; when true (disk-usage watchdog), age is
// ignored and every unused, allow-listed file goes. Only regular files
// matching an allow-listed extension are ever touched; everything else
// (including any nested directories TDLib creates) is left alone.
func cleanupMediaCache(tdDatabaseDir string, inUse map[string]struct{}, urgent bool) {
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

			if !urgent {
				info, err := d.Info()
				if err != nil {
					return nil
				}
				if info.ModTime().After(cutoff) {
					return nil
				}
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
			slog.Info("[Cleanup] media cache directory swept", "dir", dir, "urgent", urgent, "removed", removed, "skippedInUse", skippedInUse, "failed", failed)
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
// that aren't currently referenced by any chat's queue (inUse) - the
// .tmp/.part/.ogg/etc scratch files this bot's own downloader writes while
// fetching a track, plus finished downloads once nothing is playing/queuing
// them anymore. When urgent is false, only files untouched for
// cleanupMaxAge are removed; when true, age is ignored. Subdirectories and
// dotfiles are left untouched, so this is safe to run even when
// DownloadsDir happens to point at the same root as TDLib's own storage.
func cleanupDownloads(inUse map[string]struct{}, urgent bool) {
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

		if !urgent {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(cutoff) {
				continue
			}
		}

		if err := os.Remove(path); err != nil {
			failed++
			slog.Warn("[Cleanup] failed to remove stale download", "file", path, "error", err)
			continue
		}
		removed++
	}

	if removed > 0 || failed > 0 || skippedInUse > 0 {
		slog.Info("[Cleanup] downloads directory swept", "urgent", urgent, "removed", removed, "skippedInUse", skippedInUse, "failed", failed)
	}
}
