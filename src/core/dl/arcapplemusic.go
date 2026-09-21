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
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/utils"
)

// arcAppleMusic is a dedicated client for the ArcMusic API's Apple Music
// endpoints (/applemusic/search, /applemusic/download). It shares the same
// ARC_API_URL / ARC_API_KEY configuration already used by arcMusic for
// YouTube (see arcmusic.go) and arcSpotify for Spotify (see arcspotify.go) -
// this is the same backend (YT-API), just another route family, so no
// separate credentials are needed.
//
// Unlike the generic apiData (API_URL) Apple Music flow, the ArcMusic
// /applemusic/download endpoint resolves the direct MP3 CDN link itself
// (via aplmate.com under the hood), so no further processing is needed on
// this path - it always takes the processDirectDL route (see helpers.go).
type arcAppleMusic struct {
	Query  string
	ApiUrl string
	ApiKey string
}

var (
	// arcAppleMusicSongRegex matches a single-track Apple Music URL: either a
	// canonical .../song/<name>/<id> link, or an album link carrying a
	// ?i=<trackId> query parameter (Apple's "song within an album" deep
	// link), mirroring YT-API's applemusic.py _parse_url song detection.
	arcAppleMusicSongRegex = regexp.MustCompile(
		`(?i)^(https?://)?music\.apple\.com/[a-z]{2}/song/[^/?#]+/\d+(\?.*)?$`,
	)
	// arcAppleMusicCollectionRegex matches an album or playlist Apple Music
	// URL (without a ?i= track deep link, which is handled as a song above).
	arcAppleMusicCollectionRegex = regexp.MustCompile(
		`(?i)^(https?://)?music\.apple\.com/[a-z]{2}/(album|playlist)/[^/?#]+/(\d+|pl\.[a-z0-9]+)(\?.*)?$`,
	)
	arcAppleMusicIQueryRegex = regexp.MustCompile(`(?i)[?&]i=\d+`)
)

// newArcAppleMusic creates a new ArcMusic Apple Music client using the
// configured ARC_API_URL / ARC_API_KEY.
func newArcAppleMusic(query string) *arcAppleMusic {
	return &arcAppleMusic{
		Query:  strings.TrimSpace(query),
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

// isConfigured reports whether the ArcMusic API has been configured.
func (a *arcAppleMusic) isConfigured() bool {
	return a.ApiUrl != "" && a.ApiKey != ""
}

// isSongURL reports whether the query is a single-track Apple Music link.
func (a *arcAppleMusic) isSongURL() bool {
	if arcAppleMusicSongRegex.MatchString(a.Query) {
		return true
	}
	return arcAppleMusicCollectionRegex.MatchString(a.Query) && arcAppleMusicIQueryRegex.MatchString(a.Query)
}

// isCollectionURL reports whether the query is an album/playlist Apple
// Music link (i.e. not a single-track deep link).
func (a *arcAppleMusic) isCollectionURL() bool {
	return arcAppleMusicCollectionRegex.MatchString(a.Query) && !arcAppleMusicIQueryRegex.MatchString(a.Query)
}

// isValid reports whether the query is an Apple Music URL the ArcMusic
// /applemusic endpoints can resolve.
func (a *arcAppleMusic) isValid() bool {
	if !a.isConfigured() || a.Query == "" {
		return false
	}
	return a.isSongURL() || a.isCollectionURL()
}

// arcAppleMusicTrack models a single track item returned by
// /applemusic/search.
type arcAppleMusicTrack struct {
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Duration  string `json:"duration"` // "m:ss"
	Thumbnail string `json:"thumbnail"`
	TrackUrl  string `json:"track_url"`
}

// arcAppleMusicSearchResponse models the response of /applemusic/search.
type arcAppleMusicSearchResponse struct {
	Success bool                  `json:"success"`
	Type    string                `json:"type"`
	Url     string                `json:"url"`
	Total   int                   `json:"total"`
	Tracks  []arcAppleMusicTrack  `json:"tracks"`
}

// arcAppleMusicDownloadResponse models the response of
// /applemusic/download.
type arcAppleMusicDownloadResponse struct {
	Success   bool   `json:"success"`
	Url       string `json:"url"`
	Title     string `json:"title"`
	Duration  string `json:"duration"` // "m:ss"
	Thumbnail string `json:"thumbnail"`
	TrackUrl  string `json:"track_url"`
	Cdn       string `json:"cdn"`
	Error     string `json:"error"`
}

// fetchSearch calls /applemusic/search for the given Apple Music link.
func (a *arcAppleMusic) fetchSearch(link string) (*arcAppleMusicSearchResponse, error) {
	endpoint := fmt.Sprintf("%s/applemusic/search", a.ApiUrl)
	params := url.Values{"url": {link}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcAppleMusic search request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcAppleMusic search read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcAppleMusic search status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcAppleMusicSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcAppleMusic search decode: %w", err)
	}

	if !data.Success || len(data.Tracks) == 0 {
		return nil, errors.New("arcAppleMusic search returned no tracks")
	}

	return &data, nil
}

// fetchDownload calls /applemusic/download for the given Apple Music link.
func (a *arcAppleMusic) fetchDownload(link string) (*arcAppleMusicDownloadResponse, error) {
	endpoint := fmt.Sprintf("%s/applemusic/download", a.ApiUrl)
	params := url.Values{"url": {link}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcAppleMusic download request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcAppleMusic download read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcAppleMusic download status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcAppleMusicDownloadResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcAppleMusic download decode: %w", err)
	}

	if !data.Success || data.Cdn == "" {
		errMsg := data.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("arcAppleMusic download failed: %s", errMsg)
	}

	return &data, nil
}

// getInfo retrieves metadata for an Apple Music track/album/playlist link.
func (a *arcAppleMusic) getInfo() (utils.PlatformTracks, error) {
	if !a.isValid() {
		return utils.PlatformTracks{}, errors.New("the provided URL is not a supported Apple Music link")
	}

	if a.isCollectionURL() {
		return a.getCollectionInfo()
	}

	return a.getTrackInfoAsTracks()
}

// getCollectionInfo resolves an Apple Music album/playlist link into its
// track listing via /applemusic/search. Each returned track's Url is an
// Apple Music song link, which is later re-resolved (one at a time, via
// getTrack) into an actual playable CDN link when that track comes up for
// playback - same pattern as arcSpotify.getPlaylistInfo.
func (a *arcAppleMusic) getCollectionInfo() (utils.PlatformTracks, error) {
	data, err := a.fetchSearch(a.Query)
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	tracks := make([]utils.MusicTrack, 0, len(data.Tracks))
	for _, t := range data.Tracks {
		if t.TrackUrl == "" {
			continue
		}
		tracks = append(tracks, utils.MusicTrack{
			Title:    t.Title,
			Url:      t.TrackUrl,
			Thumbnail: t.Thumbnail,
			Duration: durationToSeconds(t.Duration),
			Channel:  t.Artist,
			Platform: utils.Apple,
		})
	}

	if len(tracks) == 0 {
		return utils.PlatformTracks{}, errors.New("arcAppleMusic search resolved no playable tracks")
	}

	return utils.PlatformTracks{Results: tracks}, nil
}

// getTrackInfoAsTracks resolves a single Apple Music song link into a
// one-item PlatformTracks, matching the shape the rest of the codebase
// expects from a direct-link lookup.
func (a *arcAppleMusic) getTrackInfoAsTracks() (utils.PlatformTracks, error) {
	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	link := data.TrackUrl
	if link == "" {
		link = a.Query
	}

	track := utils.MusicTrack{
		Title:     data.Title,
		Url:       link,
		Thumbnail: data.Thumbnail,
		Duration:  durationToSeconds(data.Duration),
		Platform:  utils.Apple,
	}

	return utils.PlatformTracks{Results: []utils.MusicTrack{track}}, nil
}

// search is not supported by the ArcMusic Apple Music endpoints - there's
// no text-search route, only direct track/album/playlist link resolution.
// isValid only ever matches a direct link, so this is never actually
// reached through NewDownloaderWrapper's normal selection flow.
func (a *arcAppleMusic) search() (utils.PlatformTracks, error) {
	return utils.PlatformTracks{}, errors.New("arcAppleMusic: text search is not supported, provide a direct Apple Music link")
}

// getTrack resolves a single Apple Music song link into playable TrackInfo
// via /applemusic/download. The Key field is intentionally left empty -
// the ArcMusic endpoint already returns a ready-to-stream CDN URL, so
// download.Process() takes the processDirectDL path (see helpers.go).
func (a *arcAppleMusic) getTrack() (utils.TrackInfo, error) {
	if !a.isConfigured() {
		return utils.TrackInfo{}, errors.New("ArcMusic API is not configured")
	}

	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.TrackInfo{}, err
	}

	link := data.TrackUrl
	if link == "" {
		link = a.Query
	}

	return utils.TrackInfo{
		URL:      link,
		CdnURL:   data.Cdn,
		Platform: utils.Apple,
	}, nil
}

// downloadTrack resolves the final playable path/URL for a track fetched
// via getTrack. video is ignored - Apple Music tracks are always audio.
func (a *arcAppleMusic) downloadTrack(info utils.TrackInfo, _ bool) (string, error) {
	downloader, err := newDownload(info)
	if err != nil {
		return "", fmt.Errorf("failed to initialize the download: %w", err)
	}

	return downloader.Process()
}
