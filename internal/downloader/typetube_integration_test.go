package downloader

import (
	"os"
	"testing"

	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/utils"
)

func TestTypeTubeIntegration(t *testing.T) {
	config.DownloadsDir = "test_downloads"
	_ = os.MkdirAll(config.DownloadsDir, 0755)
	defer os.RemoveAll(config.DownloadsDir)

	// 1. Test search via TypeTube
	t.Run("Search", func(t *testing.T) {
		w := NewDlWrapper("never gonna give you up")
		res, err := w.Search()
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(res.Results) == 0 {
			t.Fatalf("No results returned")
		}
		first := res.Results[0]
		t.Logf("Found track: Title=%s, ID=%s, URL=%s", first.Title, first.Id, first.Url)
		if first.Id == "" {
			t.Errorf("Expected valid ID, got empty")
		}
	})

	// 2. Test GetInfo via TypeTube
	t.Run("GetInfo", func(t *testing.T) {
		w := NewDlWrapper("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		res, err := w.GetInfo()
		if err != nil {
			t.Fatalf("GetInfo failed: %v", err)
		}
		if len(res.Results) == 0 {
			t.Fatalf("No results returned")
		}
		track := res.Results[0]
		t.Logf("Resolved track: Title=%s, ID=%s, Duration=%d", track.Title, track.Id, track.Duration)
		if track.Id != "dQw4w9WgXcQ" {
			t.Errorf("Expected dQw4w9WgXcQ, got %s", track.Id)
		}
	})

	// 3. Test DownloadTrack via TypeTube
	t.Run("DownloadTrack", func(t *testing.T) {
		w := NewDlWrapper("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		trackInfo := &utils.TrackInfo{
			Id:       "dQw4w9WgXcQ",
			URL:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			Platform: utils.YouTube,
		}
		filePath, err := w.DownloadTrack(trackInfo, false)
		if err != nil {
			t.Fatalf("DownloadTrack failed: %v", err)
		}
		t.Logf("Downloaded file path: %s", filePath)
		stat, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Failed to stat downloaded file: %v", err)
		}
		t.Logf("Downloaded file size: %d bytes (%.2f MB)", stat.Size(), float64(stat.Size())/(1024*1024))
		if stat.Size() < 100000 {
			t.Errorf("File too small: %d bytes", stat.Size())
		}
	})

	// 4. Test Spotify URL routes to apiData
	t.Run("SpotifyRouting", func(t *testing.T) {
		config.ApiKey = "test_key"
		w := NewDlWrapper("https://open.spotify.com/track/4cOdK2wGLETKBW3PvgPWqT")
		if _, ok := w.service.(*apiData); !ok {
			t.Errorf("Expected Spotify to route to apiData, got %T", w.service)
		}
	})
}
