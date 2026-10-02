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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/db"
	"ashokshau/tgmusic/internal/utils"

	"github.com/playeon/typetube-go"
)

var (
	typeTubeClient     *typetube.Client
	typeTubeClientOnce sync.Once
)

func getTypeTubeClient() *typetube.Client {
	typeTubeClientOnce.Do(func() {
		typeTubeClient = typetube.NewClient(
			typetube.WithAPIKey(config.TypeTubeApiKey),
			typetube.WithFormat("protobuf"),
			typetube.WithHost(config.TypeTubeHost),
			typetube.WithTimeout(25*time.Second),
		)
	})
	return typeTubeClient
}

type youTubeData struct {
	Query    string
	Patterns map[string]*regexp.Regexp
	resolved *typetube.TrackResult
}

var youtubePatterns = map[string]*regexp.Regexp{
	"youtube":   regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?youtube\.com/.*`),
	"youtu_be":  regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?youtu\.be/.*`),
	"yt_music":  regexp.MustCompile(`(?i)^(?:https?://)?music\.youtube\.com/.*`),
	"yt_shorts": regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?youtube\.com/shorts/.*`),
}

func newYouTubeData(query string) *youTubeData {
	return &youTubeData{
		Query:    strings.TrimSpace(query),
		Patterns: youtubePatterns,
	}
}

func (y *youTubeData) isValid() bool {
	if y.Query == "" {
		slog.Info("The query or patterns are empty.")
		return false
	}

	for _, pattern := range y.Patterns {
		if pattern.MatchString(y.Query) {
			return true
		}
	}
	return false
}

func (y *youTubeData) getInfo() (*utils.PlatformTracks, error) {
	if !y.isValid() {
		return nil, errors.New("the provided URL is invalid or the platform is not supported")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	y.Query = normalizeYouTubeURL(y.Query)
	videoID := extractVideoID(y.Query)
	playlistID := extractPlaylistID(y.Query)

	switch {
	case playlistID != "":
		if strings.HasPrefix(playlistID, "RD") {
			return GetYouTubeMixPlaylist(ctx, playlistID)
		}
		return getYouTubePlaylist(ctx, playlistID)

	case videoID != "":
		// 1. Prioritize TypeTube resolution
		client := getTypeTubeClient()
		startResolve := time.Now()
		ttTrack, ttErr := client.Resolve(ctx, videoID)
		resolveDur := time.Since(startResolve)
		if ttErr == nil && ttTrack != nil && ttTrack.ID != "" {
			go db.Instance.RecordTypeTubeResolveSuccess(videoID, resolveDur)
			y.resolved = ttTrack
			return &utils.PlatformTracks{
				Results: []utils.GetUrlTrack{
					{
						Title:     ttTrack.Title,
						Id:        ttTrack.ID,
						Url:       fmt.Sprintf("https://www.youtube.com/watch?v=%s", ttTrack.ID),
						Thumbnail: ttTrack.Thumbnail,
						Duration:  int32(ttTrack.DurationSeconds),
						Channel:   ttTrack.Author,
						Platform:  utils.YouTube,
					},
				},
			}, nil
		}
		go db.Instance.RecordTypeTubeResolveFailure(videoID, ttErr)
		slog.Warn("TypeTube resolve failed, falling back to ArcMusic/InnerTube", "video_id", videoID, "error", ttErr)

		// 2. Fallback to ArcMusic search
		arcTracks, arcErr := newArcMusic().search(fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID), 1)
		recordArcSearch(arcErr != nil || len(arcTracks) == 0)
		if arcErr == nil && len(arcTracks) > 0 {
			return &utils.PlatformTracks{Results: arcTracks}, nil
		}

		// 3. Fallback to InnerTube search
		for _, query := range []string{videoID, y.Query} {
			tracks, err := searchYouTube(query, 10)
			if err != nil {
				continue
			}

			for _, track := range tracks {
				if track.Id == videoID {
					return &utils.PlatformTracks{Results: []utils.GetUrlTrack{track}}, nil
				}
			}
		}

		if title, err := getYouTubeTitleFromOEmbed(videoID); err == nil && title != "" {
			tracks, err := searchYouTube(title, 10)
			if err == nil {
				for _, track := range tracks {
					if track.Id == videoID {
						return &utils.PlatformTracks{Results: []utils.GetUrlTrack{track}}, nil
					}
				}
			}
		}

		slog.Warn("InnerTube exhausted, attempting getYouTubeVideo", "video_id", videoID)
		return getYouTubeVideo(ctx, videoID)
	}

	return nil, errors.New("no video or playlist results were found")
}

func (y *youTubeData) search() (*utils.PlatformTracks, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Directly resolve query via TypeTube (basic text queries or links)
	client := getTypeTubeClient()
	startResolve := time.Now()
	ttTrack, ttErr := client.Resolve(ctx, y.Query)
	resolveDur := time.Since(startResolve)
	if ttErr == nil && ttTrack != nil && ttTrack.ID != "" {
		go db.Instance.RecordTypeTubeResolveSuccess(y.Query, resolveDur)
		y.resolved = ttTrack
		thumb := ttTrack.Thumbnail
		if thumb == "" {
			thumb = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", ttTrack.ID)
		}
		track := utils.GetUrlTrack{
			Title:     ttTrack.Title,
			Id:        ttTrack.ID,
			Url:       fmt.Sprintf("https://www.youtube.com/watch?v=%s", ttTrack.ID),
			Thumbnail: thumb,
			Duration:  int32(ttTrack.DurationSeconds),
			Channel:   ttTrack.Author,
			Platform:  utils.YouTube,
		}
		return &utils.PlatformTracks{Results: []utils.GetUrlTrack{track}}, nil
	}
	go db.Instance.RecordTypeTubeResolveFailure(y.Query, ttErr)
	slog.Warn("TypeTube direct resolve failed, falling back to ArcMusic", "query", y.Query, "error", ttErr)

	// 2. Fallback to ArcMusic API search
	arcTracks, arcErr := newArcMusic().search(y.Query, 5)
	recordArcSearch(arcErr != nil || len(arcTracks) == 0)
	if arcErr == nil && len(arcTracks) > 0 {
		return &utils.PlatformTracks{Results: arcTracks}, nil
	}

	// 3. Fallback to InnerTube search
	slog.Warn("ArcMusic search failed, falling back to InnerTube search", "query", y.Query, "error", arcErr)
	tracks, err := searchYouTube(y.Query, 5)
	if err != nil {
		if arcErr != nil {
			return nil, fmt.Errorf("typetube: %v; arcmusic: %w; innertube: %v", ttErr, arcErr, err)
		}
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, errors.New("no video results were found")
	}

	return &utils.PlatformTracks{Results: tracks}, nil
}

func (y *youTubeData) getTrack() (*utils.TrackInfo, error) {
	if y.Query == "" {
		return nil, errors.New("the query is empty")
	}

	if y.resolved != nil {
		return &utils.TrackInfo{
			Id:       y.resolved.ID,
			URL:      fmt.Sprintf("https://www.youtube.com/watch?v=%s", y.resolved.ID),
			Platform: utils.YouTube,
		}, nil
	}

	if !y.isValid() {
		return nil, errors.New("the provided URL is invalid or the platform is not supported")
	}

	getInfo, err := y.getInfo()
	if err != nil {
		return nil, err
	}
	if len(getInfo.Results) == 0 {
		return nil, errors.New("no video results were found")
	}

	track := getInfo.Results[0]
	trackInfo := &utils.TrackInfo{
		Id:       track.Id,
		URL:      track.Url,
		Platform: utils.YouTube,
	}

	return trackInfo, nil
}

// downloadTrack handles downloading tracks from YouTube using TypeTube first,
// with graceful fallback to the ArcMusic API. yt-dlp has been completely removed.
func (y *youTubeData) downloadTrack(info *utils.TrackInfo, video bool) (string, error) {
	trackID := ""
	if info != nil {
		trackID = info.Id
	}
	if trackID == "" && y.resolved != nil {
		trackID = y.resolved.ID
	}
	if trackID == "" && y.Query == "" {
		return "", errors.New("track info or query is empty")
	}

	// 1. If audio (!video), prioritize TypeTube range downloader directly
	if !video {
		destFile := filepath.Join(config.DownloadsDir, fmt.Sprintf("%s.m4a", trackID))
		_ = os.Remove(destFile) // Always fresh download, skip local cache

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		client := getTypeTubeClient()
		var target interface{}
		if y.resolved != nil {
			target = y.resolved // Pass the resolved TrackResult directly to skip resolve!
		} else if trackID != "" {
			target = trackID
		} else if info != nil && info.URL != "" {
			target = info.URL
		} else {
			target = y.Query
		}

		startDl := time.Now()
		res, err := client.DownloadAudio(ctx, target, destFile, typetube.DownloadConfig{
			Workers:        16,
			ChunkSizeBytes: 2 * 1024 * 1024, // 2MB chunk size
			Quality:        "128kbps",       // Non-hi-fi (128kbps) optimal for voice chats
		})
		dlDur := time.Since(startDl)
		if err == nil && res != nil && res.FilePath != "" {
			if stat, statErr := os.Stat(res.FilePath); statErr == nil && stat.Size() > 0 {
				go db.Instance.RecordTypeTubeDownloadSuccess(trackID, dlDur, stat.Size())
				return res.FilePath, nil
			}
		}

		go db.Instance.RecordTypeTubeDownloadFailure(trackID, err)
		slog.Warn("TypeTube audio download failed, falling back to ArcMusic API", "track_id", trackID, "error", err)
	}

	// 2. Fallback to ArcMusic API (handles both audio fallback and video)
	targetID := trackID
	if targetID == "" {
		targetID = extractVideoID(y.Query)
	}
	filePath, err := newArcMusic().resolve(targetID, video)
	if err == nil {
		return filePath, nil
	}
	slog.Warn("ArcMusic resolve failed", "video_id", targetID, "video", video, "error", err)
	recordArcFallback()

	// 3. If video playback and ArcMusic failed, attempt direct TypeTube video resolution
	if video {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		client := getTypeTubeClient()
		startVid := time.Now()
		res, ttErr := client.Resolve(ctx, info.Id)
		vidDur := time.Since(startVid)
		if ttErr == nil && res != nil {
			go db.Instance.RecordTypeTubeResolveSuccess(info.Id, vidDur)
			// Find progressive video stream with audio if available
			for _, stream := range res.VideoStreams {
				if strings.Contains(stream.MimeType, "mp4a") && stream.URL != "" {
					return stream.URL, nil
				}
			}
			if res.BestVideo != nil && res.BestVideo.URL != "" {
				return res.BestVideo.URL, nil
			}
		} else {
			go db.Instance.RecordTypeTubeResolveFailure(info.Id, ttErr)
		}
	}

	return "", fmt.Errorf("failed to download track %s: %w", info.Id, err)
}
