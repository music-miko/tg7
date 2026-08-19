/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"ashokshau/tgmusic/config"
	"ashokshau/tgmusic/src/utils"
)

// musicService defines a standard interface for interacting with various music services.
// This allows for a unified approach to handling different platforms like YouTube, Spotify, etc.
type musicService interface {
	// isValid determines if the service can handle the given query.
	isValid() bool
	// getInfo retrieves metadata for a track or playlist.
	getInfo() (utils.PlatformTracks, error)
	// search queries the service for a track.
	search() (utils.PlatformTracks, error)
	// getTrack fetches detailed information for a single track.
	getTrack() (utils.TrackInfo, error)
	// downloadTrack handles the download of a track.
	downloadTrack(trackInfo utils.TrackInfo, video bool) (string, error)
}

// DownloaderWrapper provides a unified interface for music service interactions.
type DownloaderWrapper struct {
	service musicService
}

// NewDownloaderWrapper selects the appropriate musicService based on the query format or configuration defaults.
func NewDownloaderWrapper(query string) *DownloaderWrapper {
	yt := newYouTubeData(query)
	arcSp := newArcSpotify(query)
	api := newApiData(query)
	direct := newDirectLink(query)

	var chosen musicService
	switch {
	case yt.isValid():
		chosen = yt
	case arcSp.isValid():
		// Spotify track/playlist links go through ArcMusic's dedicated
		// /spotify/playlist + /spotify/download endpoints when ARC_API_URL /
		// ARC_API_KEY are configured (see arcspotify.go). Spotify album/
		// artist links aren't supported by those endpoints, so arcSp.isValid
		// only matches track/playlist and everything else falls through to
		// the generic apiData client below, same as when ArcMusic isn't
		// configured at all.
		chosen = arcSp
	case api.isValid():
		chosen = api
	case direct.isValid():
		chosen = direct
	default:
		switch config.DefaultService {
		case "spotify":
			chosen = api
		default:
			chosen = yt
		}
	}

	return &DownloaderWrapper{
		service: chosen,
	}
}

// IsValid checks if the underlying service can handle the query.
func (d *DownloaderWrapper) IsValid() bool {
	return d.service != nil && d.service.isValid()
}

// GetInfo retrieves metadata by delegating the call to the wrapped service.
func (d *DownloaderWrapper) GetInfo() (utils.PlatformTracks, error) {
	return d.service.getInfo()
}

// Search performs a search by delegating the call to the wrapped service.
func (d *DownloaderWrapper) Search() (utils.PlatformTracks, error) {
	return d.service.search()
}

// GetTrack retrieves detailed track information by delegating the call to the wrapped service.
func (d *DownloaderWrapper) GetTrack() (utils.TrackInfo, error) {
	return d.service.getTrack()
}

// DownloadTrack downloads a track by delegating the call to the wrapped service.
// It returns the file path of the downloaded track or an error if the download fails.
func (d *DownloaderWrapper) DownloadTrack(info utils.TrackInfo, video bool) (string, error) {
	return d.service.downloadTrack(info, video)
}
