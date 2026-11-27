/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/utils"
)

// arcMusic is a dedicated client for the ArcMusic API's YouTube endpoints
// (/youtube/v2/*). It is used for YouTube downloads and as a fallback for
// YouTube search; every other platform keeps using the generic API_URL client.
type arcMusic struct {
	ApiUrl string
	ApiKey string
}

const (
	arcCreateRetries  = 3
	arcPollRetries    = 10
	arcPollInterval   = 3 * time.Second
	arcDownloadCycles = 2
	arcCycleDelay     = 2 * time.Second

	// arcFileDownloadTimeout bounds only the final CDN file fetch; the shared
	// downloadTimeout is too short for large YouTube tracks/videos.
	arcFileDownloadTimeout = 90 * time.Second
)

// newArcMusic creates an ArcMusic client from ARC_API_URL / ARC_API_KEY.
func newArcMusic() *arcMusic {
	return &arcMusic{
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

// isConfigured reports whether an ArcMusic API URL is set.
func (a *arcMusic) isConfigured() bool {
	return a.ApiUrl != ""
}

// arcDownloadResponse models /youtube/v2/download. It either resolves inline
// (cache hit: status "success" with result.cdn) or is queued as a job
// (status "queued" with job_id) that must be polled.
type arcDownloadResponse struct {
	Status string `json:"status"`
	JobId  string `json:"job_id"`
	Result struct {
		Cdn string `json:"cdn"`
	} `json:"result"`
}

// arcJobStatusResponse models the job-status (poll) endpoint.
type arcJobStatusResponse struct {
	Status string `json:"status"`
	Job    struct {
		Status string `json:"status"`
		Result struct {
			Cdn string `json:"cdn"`
		} `json:"result"`
	} `json:"job"`
}

// requestDownload calls /youtube/v2/download and returns the raw response.
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
		resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
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
			lastErr = fmt.Errorf("decode v2/download response: %w", err)
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

// pollJob polls the job-status endpoint until the job is done and returns its CDN link.
func (a *arcMusic) pollJob(jobID string) (string, error) {
	endpoint := fmt.Sprintf("%s/youtube/jobStatus", a.ApiUrl)
	params := url.Values{"job_id": {jobID}}

	var lastErr error
	for attempt := 0; attempt < arcPollRetries; attempt++ {
		resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
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
			lastErr = fmt.Errorf("decode jobStatus response: %w", err)
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

// arcSearchResult models one item of /youtube/v2/search. The API uses
// "video_id" and a "m:ss" string duration, converted below.
type arcSearchResult struct {
	VideoId   string `json:"video_id"`
	Title     string `json:"title"`
	Duration  string `json:"duration"`
	Views     string `json:"views"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
	Url       string `json:"url"`
}

type arcSearchResponse struct {
	Status  string            `json:"status"`
	Results []arcSearchResult `json:"results"`
}

// durationToSeconds converts "m:ss" or "h:mm:ss" to seconds.
func durationToSeconds(d string) int32 {
	var total int32
	for _, p := range strings.Split(d, ":") {
		n := 0
		_, _ = fmt.Sscanf(p, "%d", &n)
		total = total*60 + int32(n)
	}
	return total
}

// search calls /youtube/v2/search and maps the results to utils.GetUrlTrack.
func (a *arcMusic) search(query string, limit int) ([]utils.GetUrlTrack, error) {
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

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("arcMusic search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcMusic search read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcMusic search returned HTTP %d", resp.StatusCode)
	}

	var data arcSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcMusic search decode: %w", err)
	}

	if data.Status != "success" || len(data.Results) == 0 {
		return nil, errors.New("arcMusic search returned no results")
	}

	tracks := make([]utils.GetUrlTrack, 0, len(data.Results))
	for _, r := range data.Results {
		if r.VideoId == "" {
			continue
		}
		trackURL := r.Url
		if trackURL == "" {
			trackURL = ytWatchURL + r.VideoId
		}
		tracks = append(tracks, utils.GetUrlTrack{
			Id:        r.VideoId,
			Title:     r.Title,
			Url:       trackURL,
			Thumbnail: r.Thumbnail,
			Duration:  durationToSeconds(r.Duration),
			Channel:   r.Channel,
			Views:     r.Views,
			Platform:  utils.YouTube,
		})
	}
	if len(tracks) == 0 {
		return nil, errors.New("arcMusic search returned no usable results")
	}
	return tracks, nil
}

// resolve downloads a YouTube track through the ArcMusic API. The returned
// path is either a local file, or a public Telegram post link
// (https://t.me/<username>/<msg_id>) that downloadViaWrapper then fetches
// with the bot's own Telegram session.
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
			log.Warnf("ArcMusic download request failed for %s (cycle %d): %v", videoID, cycle+1, err)
			if cycle == 0 {
				time.Sleep(arcCycleDelay)
			}
			continue
		}

		cdn := resp.Result.Cdn
		if resp.JobId != "" {
			cdn, err = a.pollJob(resp.JobId)
			if err != nil {
				lastErr = fmt.Errorf("poll job: %w", err)
				log.Warnf("ArcMusic job poll failed for %s (cycle %d): %v", videoID, cycle+1, err)
				if cycle == 0 {
					time.Sleep(arcCycleDelay)
				}
				continue
			}
		}

		if utils.TelegramMessageRegex.MatchString(cdn) {
			recordArcSuccess(isVideo, time.Since(start))
			return cdn, nil
		}

		ext := ".m4a"
		if isVideo {
			ext = ".mp4"
		}
		fileName, err := determineFilename(cdn, "")
		if err != nil {
			lastErr = fmt.Errorf("choose filename: %w", err)
			continue
		}
		if !strings.HasSuffix(fileName, ext) {
			fileName = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ext
		}

		filePath, err := downloadFileWithTimeout(cdn, fileName, false, arcFileDownloadTimeout)
		if err != nil {
			lastErr = fmt.Errorf("save file: %w", err)
			log.Warnf("ArcMusic file download failed for %s (cycle %d): %v", videoID, cycle+1, err)
			if cycle == 0 {
				time.Sleep(arcCycleDelay)
			}
			continue
		}

		recordArcSuccess(isVideo, time.Since(start))
		return filePath, nil
	}

	if lastErr == nil {
		lastErr = errors.New("ArcMusic download failed after all cycles")
	}
	recordArcFailure(isVideo, lastErr)
	return "", lastErr
}
