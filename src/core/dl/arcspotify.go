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

// arcSpotify is a dedicated client for the ArcMusic API's Spotify endpoints
// (/spotify/playlist, /spotify/download). It shares the same ARC_API_URL /
// ARC_API_KEY configuration already used by arcMusic for YouTube (see
// arcmusic.go) - this is the same backend, just a different route family,
// so no separate credentials are needed.
//
// Unlike the generic apiData (API_URL) Spotify flow (spotify_dl.go), the
// ArcMusic /spotify/download endpoint hands back an already-decrypted,
// directly-playable CDN link instead of an encrypted CDN blob + AES key -
// so no local decrypt/rebuild/ffmpeg-reencode step is needed on this path.
type arcSpotify struct {
	Query  string
	ApiUrl string
	ApiKey string
}

var (
	arcSpotifyTrackRegex    = regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*spotify\.com/track/[a-zA-Z0-9]+(\?.*)?$`)
	arcSpotifyPlaylistRegex = regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*spotify\.com/playlist/[a-zA-Z0-9]+(\?.*)?$`)
	arcSpotifyIdRegex       = regexp.MustCompile(`(?:track|playlist)/([A-Za-z0-9]+)`)
)

// newArcSpotify creates a new ArcMusic Spotify client using the configured
// ARC_API_URL / ARC_API_KEY.
func newArcSpotify(query string) *arcSpotify {
	return &arcSpotify{
		Query:  strings.TrimSpace(query),
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

// isConfigured reports whether the ArcMusic API has been configured.
func (a *arcSpotify) isConfigured() bool {
	return a.ApiUrl != "" && a.ApiKey != ""
}

// isValid reports whether the query is a Spotify track or playlist link that
// the ArcMusic /spotify endpoints can resolve. Album and artist links aren't
// supported by these endpoints, so they're left to fall through to the
// generic apiData (API_URL) client instead - same as when ArcMusic isn't
// configured at all.
func (a *arcSpotify) isValid() bool {
	if !a.isConfigured() || a.Query == "" {
		return false
	}
	return arcSpotifyTrackRegex.MatchString(a.Query) || arcSpotifyPlaylistRegex.MatchString(a.Query)
}

// extractSpotifyID pulls the track/playlist ID out of a Spotify URL for use
// as MusicTrack.Id / TrackInfo.Id.
func extractSpotifyID(link string) string {
	m := arcSpotifyIdRegex.FindStringSubmatch(link)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// arcSpotifyPlaylistTrack models a single entry returned by /spotify/playlist.
type arcSpotifyPlaylistTrack struct {
	Name     string `json:"name"`
	Url      string `json:"url"`
	Duration string `json:"duration"` // "HH:MM:SS"
}

// arcSpotifyPlaylistResponse models the response of /spotify/playlist.
type arcSpotifyPlaylistResponse struct {
	Status string                    `json:"status"`
	Total  int                       `json:"total"`
	Tracks []arcSpotifyPlaylistTrack `json:"tracks"`
}

// arcSpotifyDownloadResponse models the response of /spotify/download.
type arcSpotifyDownloadResponse struct {
	Success      bool   `json:"success"`
	SpotifyLink  string `json:"spotify_link"`
	SongName     string `json:"song_name"`
	ThumbnailURL string `json:"thumbnail_url"`
	Duration     string `json:"duration"` // "HH:MM:SS"
	Cdn          string `json:"cdn"`
	IsAudio      bool   `json:"is_audio"`
	Error        string `json:"error"`
}

// fetchPlaylist calls /spotify/playlist and returns the decoded response.
func (a *arcSpotify) fetchPlaylist() (*arcSpotifyPlaylistResponse, error) {
	endpoint := fmt.Sprintf("%s/spotify/playlist", a.ApiUrl)
	params := url.Values{"link": {a.Query}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcSpotify playlist request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcSpotify playlist read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcSpotify playlist status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcSpotifyPlaylistResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcSpotify playlist decode: %w", err)
	}

	if len(data.Tracks) == 0 {
		return nil, errors.New("arcSpotify playlist returned no tracks")
	}

	return &data, nil
}

// fetchDownload calls /spotify/download for the given Spotify track link and
// returns the decoded response.
func (a *arcSpotify) fetchDownload(link string) (*arcSpotifyDownloadResponse, error) {
	endpoint := fmt.Sprintf("%s/spotify/download", a.ApiUrl)
	params := url.Values{"link": {link}, "api_key": {a.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("arcSpotify download request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("arcSpotify download read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arcSpotify download status=%d body=%q", resp.StatusCode, string(body))
	}

	var data arcSpotifyDownloadResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("arcSpotify download decode: %w", err)
	}

	if !data.Success || data.Cdn == "" {
		errMsg := data.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, fmt.Errorf("arcSpotify download failed: %s", errMsg)
	}

	return &data, nil
}

// getInfo retrieves metadata for a Spotify track or playlist link.
func (a *arcSpotify) getInfo() (utils.PlatformTracks, error) {
	if !a.isValid() {
		return utils.PlatformTracks{}, errors.New("the provided URL is not a supported Spotify track/playlist link")
	}

	if arcSpotifyPlaylistRegex.MatchString(a.Query) {
		return a.getPlaylistInfo()
	}

	return a.getTrackInfoAsTracks()
}

// getPlaylistInfo resolves a Spotify playlist link into its track listing via
// /spotify/playlist. Each returned track's Url is a Spotify track link,
// which is what later gets re-resolved (one at a time, via getTrack) into an
// actual playable CDN link when that track comes up for playback.
func (a *arcSpotify) getPlaylistInfo() (utils.PlatformTracks, error) {
	data, err := a.fetchPlaylist()
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	tracks := make([]utils.MusicTrack, 0, len(data.Tracks))
	for _, t := range data.Tracks {
		if t.Url == "" {
			continue
		}
		tracks = append(tracks, utils.MusicTrack{
			Title:    t.Name,
			Id:       extractSpotifyID(t.Url),
			Url:      t.Url,
			Duration: durationToSeconds(t.Duration),
			Platform: utils.Spotify,
		})
	}

	if len(tracks) == 0 {
		return utils.PlatformTracks{}, errors.New("arcSpotify playlist resolved no playable tracks")
	}

	return utils.PlatformTracks{Results: tracks}, nil
}

// getTrackInfoAsTracks resolves a single Spotify track link into a one-item
// PlatformTracks, matching the shape the rest of the codebase expects from a
// direct-link lookup (see apiData.getInfo).
func (a *arcSpotify) getTrackInfoAsTracks() (utils.PlatformTracks, error) {
	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	link := data.SpotifyLink
	if link == "" {
		link = a.Query
	}

	track := utils.MusicTrack{
		Title:     data.SongName,
		Id:        extractSpotifyID(link),
		Url:       link,
		Thumbnail: data.ThumbnailURL,
		Duration:  durationToSeconds(data.Duration),
		Platform:  utils.Spotify,
	}

	return utils.PlatformTracks{Results: []utils.MusicTrack{track}}, nil
}

// search is not supported by the ArcMusic Spotify endpoints - there's no
// text-search route, only direct track/playlist link resolution. isValid
// only ever matches a direct link, so this is never actually reached through
// NewDownloaderWrapper's normal selection flow (plain-text Spotify searches
// keep going through the generic apiData client).
func (a *arcSpotify) search() (utils.PlatformTracks, error) {
	return utils.PlatformTracks{}, errors.New("arcSpotify: text search is not supported, provide a direct Spotify link")
}

// getTrack resolves a single Spotify track link into playable TrackInfo via
// /spotify/download. The Key field is intentionally left empty: unlike the
// generic apiData/Spotify flow (spotify_dl.go), the ArcMusic endpoint
// already returns a ready-to-stream CDN URL, so download.Process() takes the
// processDirectDL path (no AES decrypt / OGG rebuild / ffmpeg re-encode
// needed) - see helpers.go.
func (a *arcSpotify) getTrack() (utils.TrackInfo, error) {
	if !a.isConfigured() {
		return utils.TrackInfo{}, errors.New("ArcMusic API is not configured")
	}

	data, err := a.fetchDownload(a.Query)
	if err != nil {
		return utils.TrackInfo{}, err
	}

	link := data.SpotifyLink
	if link == "" {
		link = a.Query
	}

	return utils.TrackInfo{
		Id:       extractSpotifyID(link),
		URL:      link,
		CdnURL:   data.Cdn,
		Platform: utils.Spotify,
	}, nil
}

// downloadTrack resolves the final playable path/URL for a track fetched via
// getTrack. video is ignored - Spotify tracks are always audio.
func (a *arcSpotify) downloadTrack(info utils.TrackInfo, _ bool) (string, error) {
	downloader, err := newDownload(info)
	if err != nil {
		return "", fmt.Errorf("failed to initialize the download: %w", err)
	}

	return downloader.Process()
}
