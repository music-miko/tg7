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
	"sort"
	"strconv"
	"strings"
	"time"

	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/core/cache"
	"ashokshau/tgmusic/src/utils"
)

// ---------------------------------------------------------------------------
// Terabox
// ---------------------------------------------------------------------------
//
// Ported from ArcDLBot's TeraboxFlow, but adapted to how TgMusicBot actually
// consumes media. ArcDLBot's job was to hand the user buttons pointing at
// Terabox's CDN; here the bot has to play the thing, so we resolve the share
// once and stream the CDN URL straight into ffmpeg.
//
// Nothing is downloaded to disk. The resolved CDN link is returned as the
// "file path", which is exactly the contract directLink already uses and what
// download.processDirectDL() does for every other API-backed platform:
// ntgcalls' ffmpeg input accepts a URL, so a Terabox video streams with no
// local storage and no wait for a full download.
//
// Endpoint (same ArcMusic/YT-API backend as the other arc* services, so
// ARC_API_URL / ARC_API_KEY are reused - no new credentials):
//
//	GET {ARC_API_URL}/terabox/download?url=<share link>&api_key=<key>
//
//	{ "files": [ {
//	    "name": "...", "size": 123456, "duration": "12:34",
//	    "thumbnail": "https://...",
//	    "dl_cdn": "https://...",            // direct download
//	    "cdn": { "480": "https://...",      // streamable renditions
//	             "360": "https://..." }
//	} ] }

// teraboxPreferredQualities is the rendition preference order.
//
// 480p first, 360p second, as requested. The reasoning holds up: these are
// played into a Telegram voice chat that is re-encoded to 1280x720 at most
// (see vc/cmd.go), and the assistant is usually streaming dozens of chats at
// once, so pulling a larger rendition costs bandwidth and ffmpeg CPU without
// changing what anybody sees or hears. Anything above 480p is only used if
// neither preferred rendition exists.
var teraboxPreferredQualities = []string{"480", "360"}

// teraboxRegex matches the Terabox family of share domains. Kept in sync with
// ArcDLBot's classifier, which tracks the mirrors Terabox keeps spawning.
var teraboxRegex = regexp.MustCompile(
	`(?i)^https?://([a-z0-9-]+\.)*` +
		`(terabox\w*|1024tera\w*|terasharelink|terafileshare|` +
		`nephobox|4funbox|mirrobox|momerybox|freeterabox)` +
		`\.(com|app|net|co|link|cc)/.+`,
)

// teraboxItemSuffix pins a single file inside a multi-file share.
//
// A Terabox link can hold several files, so each one becomes its own queue
// entry. Rather than freezing a CDN URL into the queue - those are
// short-lived and signed, and a queued track may not play for an hour - each
// entry keeps the original share link plus its index, and re-resolves to a
// fresh CDN URL at play time.
var teraboxItemSuffix = regexp.MustCompile(`(?i)#tb=(\d+)$`)

// teraboxShareCache memoises a resolved share for a short while, so queueing
// a 10-file share does not mean 10 identical API calls, and so re-resolving
// at play time is usually free.
//
// The TTL is deliberately shorter than a typical Terabox signed-URL lifetime:
// a cached-but-expired link would fail at ffmpeg with no way to recover,
// whereas a cache miss just costs one API call.
var teraboxShareCache = cache.NewCache[[]teraboxFile](3 * time.Minute)

type teraboxFile struct {
	Name      string            `json:"name"`
	Size      flexInt           `json:"size"`
	Duration  flexDuration      `json:"duration"`
	Thumbnail string            `json:"thumbnail"`
	DlCdn     string            `json:"dl_cdn"`
	Cdn       map[string]string `json:"cdn"`
}

type teraboxResponse struct {
	Files  []teraboxFile `json:"files"`
	Error  string        `json:"error"`
	Detail string        `json:"detail"`
}

// streamURL picks the rendition to play, honouring teraboxPreferredQualities
// and falling back sensibly when neither is offered.
func (f *teraboxFile) streamURL() string {
	for _, q := range teraboxPreferredQualities {
		if link := strings.TrimSpace(f.Cdn[q]); link != "" {
			return link
		}
	}

	// Neither preferred rendition exists. Take the lowest quality on offer:
	// for a voice chat that is the right trade, and it keeps behaviour
	// predictable rather than silently jumping to 1080p.
	var qualities []string
	for q, link := range f.Cdn {
		if strings.TrimSpace(link) != "" {
			qualities = append(qualities, q)
		}
	}
	if len(qualities) > 0 {
		sort.Slice(qualities, func(i, j int) bool {
			return qualityRank(qualities[i]) < qualityRank(qualities[j])
		})
		return f.Cdn[qualities[0]]
	}

	// No streamable rendition at all - fall back to the direct download link,
	// which ffmpeg can still read over HTTP.
	return strings.TrimSpace(f.DlCdn)
}

// qualityRank turns "480"/"480p" into a sortable number.
func qualityRank(q string) int {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 1 << 30
	}
	return n
}

// title returns a display name for the file.
func (f *teraboxFile) title() string {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		return "Terabox file"
	}
	return name
}

type terabox struct {
	Query  string
	ApiUrl string
	ApiKey string
}

func newTerabox(query string) *terabox {
	return &terabox{
		Query:  strings.TrimSpace(query),
		ApiUrl: strings.TrimRight(config.ArcApiUrl, "/"),
		ApiKey: config.ArcApiKey,
	}
}

func (t *terabox) isConfigured() bool {
	return t.ApiUrl != "" && t.ApiKey != ""
}

// isValid reports whether this is a Terabox share link, with or without the
// #tb=<index> pin.
func (t *terabox) isValid() bool {
	if !t.isConfigured() || t.Query == "" {
		return false
	}
	return teraboxRegex.MatchString(t.shareURL())
}

// shareURL strips any #tb=<index> pin, giving the link to resolve.
func (t *terabox) shareURL() string {
	return teraboxItemSuffix.ReplaceAllString(t.Query, "")
}

// itemIndex returns the pinned file index, or 0 when unpinned.
func (t *terabox) itemIndex() int {
	m := teraboxItemSuffix.FindStringSubmatch(t.Query)
	if len(m) != 2 {
		return 0
	}
	idx, err := strconv.Atoi(m[1])
	if err != nil || idx < 0 {
		return 0
	}
	return idx
}

// resolve fetches (or reuses) the file listing for this share.
func (t *terabox) resolve() ([]teraboxFile, error) {
	share := t.shareURL()

	if cached, ok := teraboxShareCache.Get(share); ok && len(cached) > 0 {
		return cached, nil
	}

	endpoint := fmt.Sprintf("%s/terabox/download", t.ApiUrl)
	params := url.Values{"url": {share}, "api_key": {t.ApiKey}}

	resp, err := sendRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("terabox request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("terabox read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("terabox status=%d body=%q", resp.StatusCode, truncateForError(string(body)))
	}

	var data teraboxResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("terabox decode: %w", err)
	}

	if msg := firstNonEmpty(data.Error, data.Detail); msg != "" && len(data.Files) == 0 {
		return nil, fmt.Errorf("terabox: %s", msg)
	}

	// Keep only files we can actually play. A share full of PDFs is not an
	// error worth surfacing differently - it just has nothing for us.
	playable := make([]teraboxFile, 0, len(data.Files))
	for _, f := range data.Files {
		if f.streamURL() != "" {
			playable = append(playable, f)
		}
	}

	if len(playable) == 0 {
		return nil, errors.New("terabox: the link has no streamable files")
	}

	teraboxShareCache.Set(share, playable)
	return playable, nil
}

// getInfo expands a share into one queue entry per playable file.
func (t *terabox) getInfo() (utils.PlatformTracks, error) {
	if !t.isValid() {
		return utils.PlatformTracks{}, errors.New("the provided URL is not a supported Terabox link")
	}

	files, err := t.resolve()
	if err != nil {
		return utils.PlatformTracks{}, err
	}

	share := t.shareURL()

	// A pinned link resolves to exactly that one file.
	if teraboxItemSuffix.MatchString(t.Query) {
		idx := t.itemIndex()
		if idx >= len(files) {
			return utils.PlatformTracks{}, fmt.Errorf("terabox: file %d is no longer in this share", idx)
		}
		return utils.PlatformTracks{Results: []utils.MusicTrack{teraboxTrack(share, idx, files[idx])}}, nil
	}

	tracks := make([]utils.MusicTrack, 0, len(files))
	for i, f := range files {
		tracks = append(tracks, teraboxTrack(share, i, f))
	}
	return utils.PlatformTracks{Results: tracks}, nil
}

// teraboxTrack builds the queue entry for one file. Url carries the pinned
// share link, not the CDN URL, so playback re-resolves a fresh signed link.
func teraboxTrack(share string, idx int, f teraboxFile) utils.MusicTrack {
	return utils.MusicTrack{
		Title:     f.title(),
		Id:        fmt.Sprintf("%s#tb=%d", share, idx),
		Url:       fmt.Sprintf("%s#tb=%d", share, idx),
		Thumbnail: f.Thumbnail,
		Duration:  int(f.Duration),
		Channel:   "Terabox",
		Views:     humanBytes(int64(f.Size)),
		Platform:  utils.Terabox,
	}
}

// search has no meaning for Terabox - there is nothing to search, only share
// links to resolve. isValid only ever matches a link, so NewDownloaderWrapper
// never routes a text query here.
func (t *terabox) search() (utils.PlatformTracks, error) {
	return t.getInfo()
}

// getTrack resolves the pinned file to a playable CDN URL.
func (t *terabox) getTrack() (utils.TrackInfo, error) {
	if !t.isConfigured() {
		return utils.TrackInfo{}, errors.New("ArcMusic API is not configured")
	}

	files, err := t.resolve()
	if err != nil {
		return utils.TrackInfo{}, err
	}

	idx := t.itemIndex()
	if idx >= len(files) {
		return utils.TrackInfo{}, fmt.Errorf("terabox: file %d is no longer in this share", idx)
	}

	link := files[idx].streamURL()
	if link == "" {
		return utils.TrackInfo{}, errors.New("terabox: no streamable URL for this file")
	}

	return utils.TrackInfo{
		Id:       t.Query,
		URL:      t.Query,
		CdnURL:   link,
		Platform: utils.Terabox,
	}, nil
}

// downloadTrack returns the CDN URL as-is. Nothing is written to disk:
// download.Process() takes the processDirectDL path, and ffmpeg streams
// straight from Terabox's CDN.
func (t *terabox) downloadTrack(info utils.TrackInfo, _ bool) (string, error) {
	downloader, err := newDownload(info)
	if err != nil {
		return "", fmt.Errorf("failed to initialize the terabox stream: %w", err)
	}
	return downloader.Process()
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

// flexInt accepts a JSON number or a numeric string, because the upstream API
// is not consistent about which it sends for file sizes.
type flexInt int64

func (v *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*v = 0
		return nil
	}
	*v = flexInt(f)
	return nil
}

// flexDuration accepts seconds as a number, or "m:ss" / "h:mm:ss" as a string.
type flexDuration int

func (v *flexDuration) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	if strings.Contains(s, ":") {
		*v = flexDuration(durationToSeconds(s))
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*v = 0
		return nil
	}
	*v = flexDuration(int(f))
	return nil
}

func humanBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateForError(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
