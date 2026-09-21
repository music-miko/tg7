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

// arcJioSaavn is a dedicated client for the ArcMusic API's JioSaavn
// endpoints (/jiosaavn/search, /jiosaavn/download). It shares the same
// ARC_API_URL / ARC_API_KEY configuration already used by arcMusic for
// YouTube (see arcmusic.go), arcSpotify for Spotify (see arcspotify.go),
// and arcAppleMusic for Apple Music (see arcapplemusic.go) - this is the
// same backend (YT-API), just another route family, so no separate
// credentials are needed.
//
// Unlike the generic apiData (API_URL) JioSaavn flow, the ArcMusic
// /jiosaavn/download endpoint hands back an already-decrypted, directly
// playable 320kbps CDN link (YT-API decrypts JioSaavn's DES-encrypted
// media URL itself), so no local decrypt step is needed on this path - it
// always takes the processDirectDL route (see helpers.go).
type arcJioSaavn struct {
	Query  string
	ApiUrl string
	ApiKey string
}

var (
	// arcJioSaavnSongRegex matches a single-track JioSaavn URL.
	arcJioSaavnSongRegex = regexp.MustCompile(
		`(?i)^(https?://)?(www\.)?jiosaavn\.com/(s/)?song/[\w\-%]+/[\w\-%]+/?(\?.*)?$`,
	)
	// arcJioSaavnCollectionRegex matches an album/playlist/featured JioSaavn
	// URL (multi-track).
	arcJioSaavnCollectionRegex = regexp.MustCompile(
		`(?i)^(https?://)?(www\.)?jiosaavn\.com/(s/)?(album|playlist|featured)/[\w\-%]+/[\w\-%]+/?(\?.*)?$`,
	)
)

// newArcJioSaavn creates a new ArcMusic JioSaavn client using the
// configured ARC_API_URL / ARC_API_KEY.
func newArcJioSaavn(query string) *arcJioSaavn {
	return &arcJioSaavn{
		Query:  strings.TrimSpace(query),
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

// isConfigured reports whether the ArcMusic API has been configured.
func (a *arcJioSaavn) isConfigured() bool {
	return a.ApiUrl != "" && a.ApiKey != ""
}

// isValid reports whether the query is a JioSaavn track/album/playlist
// link the ArcMusic /jiosaavn endpoints can resolve.
func (a *arcJioSaavn) isValid() bool {
	if !a.isConfigured() || a.Query == "" {
		return false
	}
	return arcJioSaavnSongRegex.MatchString(a.Query) || arcJioSaavnCollectionRegex.MatchString(a.Query)
}

// arcJioSaavnTrack models a single track item returned by
// /jiosaavn/search.
type arcJioSaavnTrack struct {
	Title     string `json:"title"`
	Duration  string `json:"duration"` // "m:ss"
	Thumbnail string `json:"thumbnail"`
	SongUrl   string `json:"song_url"`
}

// arcJioSaavnSearchResponse models the response of /jiosaavn/search.
type arcJioSaavnSearchResponse struct {
	Success bool               `json:"success"`
	Type    string             `json:"type"`
	Url     string             `json:"url"`
	Total   int                `json:"total"`
	Tracks  []arcJioSaavnTrack `json:"tracks"`
}

// arcJioSaavnDownloadResponse models the response of /jiosaavn/download.
type arcJioSaavnDownloadResponse struct {
	Success   bool   `json:"success"`
	Url       string `json:"url"`
	Title     string `json:"title"`
	Duration  string `json:"duration"` // "m:ss"
	Thumbnail string `json:"thumbnail"`
	SongUrl   string `json:"song_url"`
	Cdn       string `json:"cdn"`
	Error     string `json:"error"`
}

// fetchSearch calls /jiosaavn/search for the given JioSaavn link.
func (a *arcJioSaavn) fetchSearch(link string) (*arcJioSaavnSearchResponse, error) {
	endpoint := fmt.Sprintf("%s/jiosaavn/search", a.ApiUrl)
	params := url.Values{"url": {link}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcJioSaavn search request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcJioSaavn search read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcJioSaavn search status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcJioSaavnSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcJioSaavn search decode: %w", err)
	}

	if !data.Success || len(data.Tracks) == 0 {
		return nil, errors.New("arcJioSaavn search returned no tracks")
	}

	return &data, nil
}

// fetchDownload calls /jiosaavn/download for the given JioSaavn song link.
// Only single-track URLs are accepted by this endpoint.
func (a *arcJioSaavn) fetchDownload(link string) (*arcJioSaavnDownloadResponse, error) {
	endpoint := fmt.Sprintf("%s/jiosaavn/download", a.ApiUrl)
	params := url.Values{"url": {link}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcJioSaavn download request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcJioSaavn download read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcJioSaavn download status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcJioSaavnDownloadResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcJioSaavn download decode: %w", err)
	}

	if !data.Success || data.Cdn == "" {
		errMsg := data.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("arcJioSaavn download failed: %s", errMsg)
	}

	return &data, nil
}

// getInfo retrieves metadata for a JioSaavn track/album/playlist link.
func (a *arcJioSaavn) getInfo() (utils.PlatformTracks, error) {
	if !a.isValid() {
		return utils.PlatformTracks{}, errors.New("the provided URL is not a supported JioSaavn link")
	}

	if arcJioSaavnCollectionRegex.MatchString(a.Query) {
		return a.getCollectionInfo()
	}

	return a.getTrackInfoAsTracks()
}

// getCollectionInfo resolves a JioSaavn album/playlist/featured link into
// its track listing via /jiosaavn/search. Each returned track's Url is a
// JioSaavn song link, which is later re-resolved (one at a time, via
// getTrack) into an actual playable CDN link when that track comes up for
// playback - same pattern as arcSpotify.getPlaylistInfo.
func (a *arcJioSaavn) getCollectionInfo() (utils.PlatformTracks, error) {
	data, err := a.fetchSearch(a.Query)
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	tracks := make([]utils.MusicTrack, 0, len(data.Tracks))
	for _, t := range data.Tracks {
		if t.SongUrl == "" {
			continue
		}
		tracks = append(tracks, utils.MusicTrack{
			Title:     t.Title,
			Url:       t.SongUrl,
			Thumbnail: t.Thumbnail,
			Duration:  durationToSeconds(t.Duration),
			Platform:  utils.JioSaavn,
		})
	}

	if len(tracks) == 0 {
		return utils.PlatformTracks{}, errors.New("arcJioSaavn search resolved no playable tracks")
	}

	return utils.PlatformTracks{Results: tracks}, nil
}

// getTrackInfoAsTracks resolves a single JioSaavn song link into a
// one-item PlatformTracks, matching the shape the rest of the codebase
// expects from a direct-link lookup.
func (a *arcJioSaavn) getTrackInfoAsTracks() (utils.PlatformTracks, error) {
	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	link := data.SongUrl
	if link == "" {
		link = a.Query
	}

	track := utils.MusicTrack{
		Title:     data.Title,
		Url:       link,
		Thumbnail: data.Thumbnail,
		Duration:  durationToSeconds(data.Duration),
		Platform:  utils.JioSaavn,
	}

	return utils.PlatformTracks{Results: []utils.MusicTrack{track}}, nil
}

// search is not supported by the ArcMusic JioSaavn endpoints - there's no
// text-search route, only direct track/album/playlist link resolution.
// isValid only ever matches a direct link, so this is never actually
// reached through NewDownloaderWrapper's normal selection flow.
func (a *arcJioSaavn) search() (utils.PlatformTracks, error) {
	return utils.PlatformTracks{}, errors.New("arcJioSaavn: text search is not supported, provide a direct JioSaavn link")
}

// getTrack resolves a single JioSaavn song link into playable TrackInfo via
// /jiosaavn/download. The Key field is intentionally left empty - the
// ArcMusic endpoint already returns a ready-to-stream, decrypted CDN URL,
// so download.Process() takes the processDirectDL path (see helpers.go).
func (a *arcJioSaavn) getTrack() (utils.TrackInfo, error) {
	if !a.isConfigured() {
		return utils.TrackInfo{}, errors.New("ArcMusic API is not configured")
	}

	if arcJioSaavnCollectionRegex.MatchString(a.Query) {
		return utils.TrackInfo{}, errors.New("only single-track JioSaavn URLs can be resolved to a playable track")
	}

	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.TrackInfo{}, err
	}

	link := data.SongUrl
	if link == "" {
		link = a.Query
	}

	return utils.TrackInfo{
		URL:      link,
		CdnURL:   data.Cdn,
		Platform: utils.JioSaavn,
	}, nil
}

// downloadTrack resolves the final playable path/URL for a track fetched
// via getTrack. video is ignored - JioSaavn tracks are always audio.
func (a *arcJioSaavn) downloadTrack(info utils.TrackInfo, _ bool) (string, error) {
	downloader, err := newDownload(info)
	if err != nil {
		return "", fmt.Errorf("failed to initialize the download: %w", err)
	}

	return downloader.Process()
}
