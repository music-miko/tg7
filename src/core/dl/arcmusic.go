/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/utils"
)

// arcMusic is a dedicated client for the ArcMusic API, used exclusively for
// resolving and downloading YouTube tracks. Other platforms (Spotify, Apple
// Music, SoundCloud, Deezer, etc.) continue to use the generic apiData client
// configured via API_URL / API_KEY.
type arcMusic struct {
	ApiUrl string
	ApiKey string
}

const (
	arcCreateRetries  = 3               // matches _api.py's create_job: for _ in range(3)
	arcPollRetries    = 10              // matches _api.py's API(retries=10) default
	arcPollInterval   = 3 * time.Second // matches _api.py's get_url: await asyncio.sleep(3)
	arcDownloadCycles = 2               // whole-pipeline retry count if the final byte-fetch step fails
	arcCycleDelay     = 2 * time.Second // delay before the pipeline is retried after a non-final failure

	// arcFileDownloadTimeout is a hard timeout applied only to the final CDN
	// file-save step of the ArcMusic (YouTube) job pipeline. The shared
	// downloadTimeout (40s) used by the generic API_URL platforms is too
	// short for big YouTube tracks/videos, which was causing ArcMusic
	// downloads to fail mid-stream. Mirrors tosu4-master's DOWNLOAD_TIMEOUT
	// pattern of using a longer hard timeout for large CDN downloads.
	arcFileDownloadTimeout = 90 * time.Second
)

// newArcMusic creates a new ArcMusic API client using the configured ARC_API_URL / ARC_API_KEY.
func newArcMusic() *arcMusic {
	return &arcMusic{
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

// isConfigured reports whether the ArcMusic API has been configured.
func (a *arcMusic) isConfigured() bool {
	return a.ApiUrl != ""
}

// arcDownloadResponse models the response of /youtube/v2/download, which can
// now resolve two ways:
//   - Cache hit: {"status":"success","job_id":null,"result":{"cdn":"...", ...}}
//     resolved inline by the API (Mongo/local-disk lookup, no scraping) - no
//     polling needed.
//   - Cache miss: {"status":"queued","job_id":"..."} - falls through to the
//     existing job-status polling flow.
type arcDownloadResponse struct {
	Status string `json:"status"`
	JobId  string `json:"job_id"`
	Result struct {
		Cdn string `json:"cdn"`
	} `json:"result"`
}

// arcJobStatusResponse models the response of the job-status (poll) endpoint.
type arcJobStatusResponse struct {
	Status string `json:"status"`
	Job    struct {
		Status string `json:"status"`
		Result struct {
			Cdn string `json:"cdn"`
		} `json:"result"`
	} `json:"job"`
}

// requestDownload calls /youtube/v2/download and returns the raw response,
// whether it resolved inline (cache hit) or was queued as a background job
// (cache miss).
func (a *arcMusic) requestDownload(videoID string, isVideo bool) (*arcDownloadResponse, error) {
	endpoint := fmt.Sprintf("%s/youtube/v2/download", a.ApiUrl)
	params := url.Values{
		"query":   {videoID},
		"isVideo": {strconv.FormatBool(isVideo)},
	}
	if a.ApiKey != "" {
		params.Set("api_key", a.ApiKey)
	}

	var lastErr error
	for attempt := 0; attempt < arcCreateRetries; attempt++ {
		resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}

		var data arcDownloadResponse
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = fmt.Errorf("failed to decode v2/download response: %w", err)
			time.Sleep(time.Second)
			continue
		}

		switch {
		case data.Status == "success" && data.Result.Cdn != "":
			return &data, nil
		case data.Status == "queued" && data.JobId != "":
			return &data, nil
		default:
			lastErr = fmt.Errorf("unexpected v2/download response: status=%q job_id=%q", data.Status, data.JobId)
			time.Sleep(time.Second)
		}
	}

	if lastErr == nil {
		lastErr = errors.New("v2/download request failed after retries")
	}
	return nil, lastErr
}

// pollJob polls the job-status endpoint until the job completes, then returns
// the CDN link of the downloaded file.
func (a *arcMusic) pollJob(jobID string) (string, error) {
	endpoint := fmt.Sprintf("%s/youtube/jobStatus", a.ApiUrl)
	params := url.Values{"job_id": {jobID}}

	var lastErr error
	for attempt := 0; attempt < arcPollRetries; attempt++ {
		resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
		if err != nil {
			lastErr = err
			time.Sleep(arcPollInterval)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(arcPollInterval)
			continue
		}

		var data arcJobStatusResponse
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = fmt.Errorf("failed to decode jobStatus response: %w", err)
			time.Sleep(arcPollInterval)
			continue
		}

		if data.Status != "success" || data.Job.Status != "done" {
			lastErr = fmt.Errorf("job not ready (status=%q, job.status=%q)", data.Status, data.Job.Status)
			time.Sleep(arcPollInterval)
			continue
		}

		if data.Job.Result.Cdn == "" {
			return "", errors.New("job completed but no cdn was returned")
		}

		return data.Job.Result.Cdn, nil
	}

	if lastErr == nil {
		lastErr = errors.New("jobStatus polling exhausted retries")
	}
	return "", lastErr
}

// arcSearchResult models a single track item returned by /youtube/v2/search.
// Note: the Python API uses "video_id" and a string duration ("3:45"), whereas
// MusicTrack uses "id" and an int duration in seconds — converted below.
type arcSearchResult struct {
	VideoId   string `json:"video_id"`
	Title     string `json:"title"`
	Duration  string `json:"duration"` // "m:ss" or "h:mm:ss"
	Views     string `json:"views"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
	Url       string `json:"url"`
}

// arcSearchResponse models the full response envelope of /youtube/v2/search.
type arcSearchResponse struct {
	Status  string            `json:"status"`
	Results []arcSearchResult `json:"results"`
}

// durationToSeconds converts a "m:ss" or "h:mm:ss" string to total seconds.
func durationToSeconds(d string) int {
	parts := strings.Split(d, ":")
	total := 0
	for _, p := range parts {
		n := 0
		fmt.Sscanf(p, "%d", &n)
		total = total*60 + n
	}
	return total
}

// search calls the ArcMusic /youtube/v2/search endpoint and returns results
// as []utils.MusicTrack, matching the shape used by the rest of the Go codebase.
func (a *arcMusic) search(query string, limit int) ([]utils.MusicTrack, error) {
	if !a.isConfigured() {
		return nil, errors.New("ArcMusic API is not configured")
	}

	endpoint := fmt.Sprintf("%s/youtube/v2/search", a.ApiUrl)
	params := url.Values{
		"query": {query},
		"limit": {strconv.Itoa(limit)},
	}
	if a.ApiKey != "" {
		params.Set("api_key", a.ApiKey)
	}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcMusic search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcMusic search read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcMusic search status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcMusic search decode: %w", err)
	}

	if data.Status != "success" || len(data.Results) == 0 {
		return nil, errors.New("arcMusic search returned no results")
	}

	tracks := make([]utils.MusicTrack, 0, len(data.Results))
	for _, r := range data.Results {
		tracks = append(tracks, utils.MusicTrack{
			Id:        r.VideoId,
			Title:     r.Title,
			Url:       r.Url,
			Thumbnail: r.Thumbnail,
			Duration:  durationToSeconds(r.Duration),
			Channel:   r.Channel,
			Views:     r.Views,
			Platform:  utils.YouTube,
		})
	}
	return tracks, nil
}

// resolve calls the ArcMusic /youtube/v2/download endpoint. The API now
// checks its own Mongo/local-disk cache inline before ever touching the
// scraping pipeline, so a cache hit comes back immediately with no job_id
// (see requestDownload) - the bot no longer needs its own direct database
// connection to shortcut this (that duplicate logic has been removed).
//
// The resulting "cdn" link is either:
//   - a public Telegram post link (https://t.me/<username>/<msg_id>) - left
//     as-is and downloaded natively via the bot's own Telegram session
//     further down the pipeline (downloadViaWrapper's TelegramMessageRegex
//     check in downloader.go).
//   - a plain HTTP CDN URL - downloaded here directly.
func (a *arcMusic) resolve(videoID string, isVideo bool) (string, error) {
	recordArcAttempt(isVideo)

	if !a.isConfigured() {
		err := errors.New("ArcMusic API is not configured")
		recordArcFailure(isVideo, err)
		return "", err
	}

	start := time.Now()

	var lastErr error
	for cycle := 0; cycle < arcDownloadCycles; cycle++ {
		resp, err := a.requestDownload(videoID, isVideo)
		if err != nil {
			lastErr = fmt.Errorf("request download: %w", err)
			slog.Warn("ArcMusic v2/download request failed", "video_id", videoID, "cycle", cycle+1, "error", err)
			if cycle == 0 {
				time.Sleep(arcCycleDelay)
			}
			continue
		}

		var cdn string
		cacheHit := resp.JobId == ""

		if cacheHit {
			cdn = resp.Result.Cdn
		} else {
			cdn, err = a.pollJob(resp.JobId)
			if err != nil {
				lastErr = fmt.Errorf("poll job: %w", err)
				slog.Warn("ArcMusic jobStatus failed", "video_id", videoID, "job_id", resp.JobId, "cycle", cycle+1, "error", err)
				if cycle == 0 {
					time.Sleep(arcCycleDelay)
				}
				continue
			}
		}

		// A public Telegram post link is downloaded natively via the bot's
		// own Telegram session further down the pipeline (see
		// downloadViaWrapper's TelegramMessageRegex check in downloader.go).
		if utils.TelegramMessageRegex.MatchString(cdn) {
			recordArcSuccess(isVideo, cacheHit, time.Since(start))
			return cdn, nil
		}

		ext := ".m4a"
		if isVideo {
			ext = ".mp4"
		}
		fileName := determineFilename(cdn, "")
		if !strings.HasSuffix(fileName, ext) {
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ext
		}

		filePath, err := downloadFileWithTimeout(cdn, fileName, false, arcFileDownloadTimeout)
		if err != nil {
			lastErr = fmt.Errorf("save file: %w", err)
			slog.Warn("ArcMusic save_file failed", "video_id", videoID, "url", cdn, "cycle", cycle+1, "error", err)
			if cycle == 0 {
				time.Sleep(arcCycleDelay)
			}
			continue
		}

		recordArcSuccess(isVideo, cacheHit, time.Since(start))
		return filePath, nil
	}

	if lastErr == nil {
		lastErr = errors.New("ArcMusic download failed after all cycles")
	}
	recordArcFailure(isVideo, lastErr)
	return "", lastErr
}
