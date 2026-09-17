package vc

import (
	"ashokshau/tgmusic/src/vc/ntgcalls"
	"fmt"
	"regexp"
	"strings"
)

var isURLRegex = regexp.MustCompile(`^https?://`)

// isHLSURL reports whether filePath points at an HLS manifest (.m3u8),
// ignoring any query string - CDN proxies commonly serve these behind a
// signed-token query (e.g. "...playlist.m3u8?token=...").
//
// This matters because HLS has its own, correct notion of "is this stream
// finite or live" baked into the manifest itself (the presence or absence of
// #EXT-X-ENDLIST) which ffmpeg's hls demuxer already honours. Our outer
// -reconnect_at_eof flag has no business overriding that - see isLive below.
func isHLSURL(filePath string) bool {
	if i := strings.IndexByte(filePath, '?'); i >= 0 {
		filePath = filePath[:i]
	}
	return strings.HasSuffix(strings.ToLower(filePath), ".m3u8")
}

// getMediaDescription creates a media description for ntgcalls based on the provided file path, video status, and ffmpeg parameters.
//
// durationSeconds is the known length of the media, or 0 when unknown. It is
// used only to tell a finite track from an endless live stream; see the
// reconnect handling below.
func getMediaDescription(filePath string, isVideo bool, durationSeconds int, ffmpegParameters string) ntgcalls.MediaDescription {
	audioDescription := &ntgcalls.AudioDescription{
		MediaSource:  ntgcalls.MediaSourceShell,
		SampleRate:   48000,
		ChannelCount: 2,
	}

	quotedPath := fmt.Sprintf("\"%s\"", filePath)
	isURL := isURLRegex.MatchString(filePath)

	// A URL with no known duration is treated as a live stream; a URL with a
	// duration is a finite track being streamed rather than downloaded (a
	// Spotify or Terabox CDN link, say).
	//
	// The distinction decides "-reconnect_at_eof". That flag makes ffmpeg
	// treat end-of-file as a dropped connection and restart the URL from the
	// beginning. For a live stream that is exactly right. For a finite track
	// it is fatal: ffmpeg never exits on real completion, ntgcalls never sees
	// a clean stream end, OnStreamEnd never fires, and the track silently
	// loops from position 0 forever instead of advancing the queue.
	//
	// tg7 previously dropped the flag altogether to stop that looping, which
	// fixed finite tracks but left live streams unable to recover from a
	// transient EOF. Gating on duration - the approach TgMusicBot-dev takes -
	// gets both cases right IF the duration is known. It isn't always: e.g.
	// Terabox's API doesn't return a per-file duration at all, so every
	// Terabox track looks like "duration unknown" here even though it is
	// finite content.
	//
	// HLS (.m3u8) is carved out unconditionally, independent of duration,
	// because it has its own correct live/VOD signal that -reconnect_at_eof
	// has no business overriding - see isHLSURL. This is also what actually
	// matters in practice: applying -reconnect_at_eof to an HLS input makes
	// ffmpeg treat the manifest's (and each segment's) legitimate EOF as a
	// dropped connection and loop trying to reconnect instead of ever
	// producing frames - so playback silently never starts. No ffmpeg error,
	// no ntgcalls error, it just sits there. A Terabox track streamed via a
	// .m3u8 CDN link hits this exactly: duration unknown (see above) plus
	// HLS is the one combination where the duration-only heuristic gets it
	// wrong in the most damaging way, so it gets its own explicit check
	// rather than relying on the CDN source to ever report a duration.
	isLive := isURL && !isHLSURL(filePath) && durationSeconds <= 0

	var audioCmd strings.Builder
	audioCmd.WriteString("ffmpeg ")
	if isLive {
		audioCmd.WriteString("-reconnect 1 -reconnect_at_eof 1 -reconnect_streamed 1 -reconnect_delay_max 2 ")
	} else if isURL {
		audioCmd.WriteString("-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 2 ")
	}

	var seekFlags, filterFlags string
	if ffmpegParameters != "" {
		if strings.Contains(ffmpegParameters, "filter:") {
			filterFlags = ffmpegParameters
		} else {
			seekFlags = ffmpegParameters
		}
	}

	if seekFlags != "" {
		audioCmd.WriteString(seekFlags + " ")
	}

	audioCmd.WriteString("-i " + quotedPath + " ")
	if filterFlags != "" {
		audioCmd.WriteString(filterFlags + " ")
	}

	audioCmd.WriteString(fmt.Sprintf("-f s16le -ac %d -ar %d -v quiet pipe:1",
		audioDescription.ChannelCount,
		audioDescription.SampleRate,
	))
	audioDescription.Input = audioCmd.String()

	if !isVideo {
		return ntgcalls.MediaDescription{
			Microphone: audioDescription,
		}
	}

	originalWidth, originalHeight := getVideoDimensions(filePath)

	width := 1280
	height := 720

	if originalWidth > 0 && originalHeight > 0 {
		ratio := float64(originalWidth) / float64(originalHeight)
		newW := min(originalWidth, width)
		newH := int(float64(newW) / ratio)

		if newH > height {
			newH = height
			newW = int(float64(newH) * ratio)
		}

		if newW%2 != 0 {
			newW--
		}
		if newH%2 != 0 {
			newH--
		}

		width = newW
		height = newH
	}

	videoDescription := &ntgcalls.VideoDescription{
		MediaSource: ntgcalls.MediaSourceShell,
		Width:       int16(width),
		Height:      int16(height),
		Fps:         30,
	}

	var videoCmd strings.Builder
	videoCmd.WriteString("ffmpeg ")

	if isLive {
		videoCmd.WriteString("-reconnect 1 -reconnect_at_eof 1 -reconnect_streamed 1 -reconnect_delay_max 2 ")
	} else if isURL {
		videoCmd.WriteString("-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 2 ")
	}

	if seekFlags != "" {
		videoCmd.WriteString(seekFlags + " ")
	}

	videoCmd.WriteString(fmt.Sprintf("-i %s ", quotedPath))
	if filterFlags != "" {
		videoCmd.WriteString(filterFlags + " ")
	}

	videoCmd.WriteString(fmt.Sprintf("-f rawvideo -r %d -pix_fmt yuv420p -vf scale=%d:%d -v quiet pipe:1",
		videoDescription.Fps,
		videoDescription.Width,
		videoDescription.Height,
	))
	videoDescription.Input = videoCmd.String()

	return ntgcalls.MediaDescription{
		Microphone: audioDescription,
		Camera:     videoDescription,
	}
}
