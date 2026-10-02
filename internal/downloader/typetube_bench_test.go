package downloader

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/playeon/typetube-go"
)

func TestBenchmarkTypeTube(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithAPIKey("tt_priv_9999999"),
		typetube.WithFormat("protobuf"),
		typetube.WithHost("https://typetube.xysushi.in"),
	)

	queries := []string{
		"Lost Sky - Fearless pt.II",
		"Never Gonna Give You Up",
		"dQw4w9WgXcQ",
	}

	for _, q := range queries {
		start := time.Now()
		res, err := client.Resolve(context.Background(), q)
		resolveDur := time.Since(start)
		if err != nil {
			t.Fatalf("Resolve failed for %s: %v", q, err)
		}
		t.Logf("[RESOLVE] Query='%s' -> Title='%s' ID=%s DurationSec=%d Latency=%v (reported LatencyMs=%.2f ms)",
			q, res.Title, res.ID, res.DurationSeconds, resolveDur, res.LatencyMs)
	}

	// Benchmark download with different configurations
	benchTrack := "Lost Sky - Fearless pt.II"
	res, err := client.Resolve(context.Background(), benchTrack)
	if err != nil {
		t.Fatalf("Resolve benchTrack failed: %v", err)
	}
	var cleanStreams []typetube.AudioStream
	for _, s := range res.AudioStreams {
		if !strings.Contains(s.URL, "c=WEB_CREATOR") {
			cleanStreams = append(cleanStreams, s)
		}
	}
	res.AudioStreams = cleanStreams

	configs := []struct {
		name      string
		workers   int
		chunkSize int64
		quality   string
	}{
		{"HiFi Default (Workers=4, Chunk=1MB)", 4, 1024 * 1024, "highest"},
		{"HiFi Fast (Workers=16, Chunk=2MB)", 16, 2 * 1024 * 1024, "highest"},
		{"Non-HiFi (128kbps, Workers=16, Chunk=2MB)", 16, 2 * 1024 * 1024, "128kbps"},
	}

	destDir := "test_bench_dl"
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(destDir)

	for _, cfg := range configs {
		destFile := fmt.Sprintf("%s/test_%d_%d.m4a", destDir, cfg.workers, cfg.chunkSize)
		_ = os.Remove(destFile)

		start := time.Now()
		dlRes, err := client.DownloadAudio(context.Background(), res, destFile, typetube.DownloadConfig{
			Workers:        cfg.workers,
			ChunkSizeBytes: cfg.chunkSize,
			Quality:        cfg.quality,
		})
		dur := time.Since(start)
		if err != nil {
			t.Errorf("[%s] Download failed: %v", cfg.name, err)
			continue
		}
		t.Logf("[%s] -> Time=%.2fs | Size=%.2fMB | Speed=%.2f MB/s (%.2f Mbps)",
			cfg.name, dur.Seconds(), dlRes.FileSizeMB, dlRes.FileSizeMB/dur.Seconds(), dlRes.SpeedMbps)
	}
}

func TestBenchmark10Tracks(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithAPIKey("tt_priv_9999999"),
		typetube.WithFormat("protobuf"),
		typetube.WithHost("https://typetube.xysushi.in"),
	)

	tracks := []string{
		"Lost Sky - Fearless pt.II",
		"Rick Astley - Never Gonna Give You Up",
		"Alan Walker - Faded",
		"The Weeknd - Blinding Lights",
		"Imagine Dragons - Believer",
		"Queen - Bohemian Rhapsody",
		"Ed Sheeran - Shape of You",
		"Cartoon - On & On",
		"Elektronomia - Sky High",
		"dQw4w9WgXcQ",
	}

	destDir := "test_10tracks_dl"
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(destDir)

	type result struct {
		query       string
		title       string
		id          string
		durationSec int
		resolveMs   int64
		serverMs    float64
		downloadSec float64
		sizeMB      float64
		speedMBs    float64
		totalSec    float64
	}

	var results []result

	for i, q := range tracks {
		destFile := fmt.Sprintf("%s/track_%d.m4a", destDir, i)
		_ = os.Remove(destFile)

		t0 := time.Now()
		res, err := client.Resolve(context.Background(), q)
		resolveDur := time.Since(t0)
		if err != nil {
			t.Fatalf("[%d] Resolve failed for '%s': %v", i+1, q, err)
		}

		t1 := time.Now()
		dlRes, err := client.DownloadAudio(context.Background(), res, destFile, typetube.DownloadConfig{
			Workers:        16,
			ChunkSizeBytes: 2 * 1024 * 1024,
			Quality:        "128kbps",
		})
		downloadDur := time.Since(t1)
		_ = os.Remove(destFile)

		if err != nil {
			t.Fatalf("[%d] Download failed for '%s': %v", i+1, q, err)
		}

		totalDur := time.Since(t0)
		speed := 0.0
		if downloadDur.Seconds() > 0 {
			speed = dlRes.FileSizeMB / downloadDur.Seconds()
		}

		r := result{
			query:       q,
			title:       res.Title,
			id:          res.ID,
			durationSec: res.DurationSeconds,
			resolveMs:   resolveDur.Milliseconds(),
			serverMs:    res.LatencyMs,
			downloadSec: downloadDur.Seconds(),
			sizeMB:      dlRes.FileSizeMB,
			speedMBs:    speed,
			totalSec:    totalDur.Seconds(),
		}
		results = append(results, r)

		t.Logf("[%02d/10] Query: %-36s | ID: %-11s | Res: %4dms (srv: %5.1fms) | DL: %4.2fs (%4.2fMB @ %4.2fMB/s) | Total: %4.2fs",
			i+1, q, r.id, r.resolveMs, r.serverMs, r.downloadSec, r.sizeMB, r.speedMBs, r.totalSec)
	}

	var totResolveMs int64
	var totDlSec float64
	var totSizeMB float64
	var totTotalSec float64

	for _, r := range results {
		totResolveMs += r.resolveMs
		totDlSec += r.downloadSec
		totSizeMB += r.sizeMB
		totTotalSec += r.totalSec
	}

	n := float64(len(results))
	avgResolve := float64(totResolveMs) / n
	avgDl := totDlSec / n
	avgSize := totSizeMB / n
	avgSpeed := totSizeMB / totDlSec
	avgTotal := totTotalSec / n

	t.Logf("=== SUMMARY (%d TRACKS) ===", len(results))
	t.Logf("Average Resolve Latency : %.1f ms", avgResolve)
	t.Logf("Average Download Time   : %.2f s (Avg Size: %.2f MB)", avgDl, avgSize)
	t.Logf("Average Download Speed  : %.2f MB/s", avgSpeed)
	t.Logf("Average Total E2E Time  : %.2f s", avgTotal)
}

func TestBenchmark50Tracks(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithAPIKey("tt_priv_9999999"),
		typetube.WithFormat("protobuf"),
		typetube.WithHost("https://typetube.xysushi.in"),
	)

	tracks := []string{
		"Lost Sky - Fearless pt.II",
		"Rick Astley - Never Gonna Give You Up",
		"Alan Walker - Faded",
		"The Weeknd - Blinding Lights",
		"Imagine Dragons - Believer",
		"Queen - Bohemian Rhapsody",
		"Ed Sheeran - Shape of You",
		"Cartoon - On & On",
		"Elektronomia - Sky High",
		"dQw4w9WgXcQ",
		"Dua Lipa - Levitating",
		"Avicii - Wake Me Up",
		"Coldplay - Viva La Vida",
		"Linkin Park - In The End",
		"Eminem - Without Me",
		"Michael Jackson - Billie Jean",
		"Post Malone - Circles",
		"Marshmello - Alone",
		"The Chainsmokers - Closer",
		"Sia - Cheap Thrills",
		"Alan Walker - Spectre",
		"K-391 - Summertime",
		"Tobu - Hope",
		"Janji - Heroes Tonight",
		"Deorro - Five Hours",
		"Martin Garrix - Animals",
		"DJ Snake - Turn Down for What",
		"David Guetta - Titanium",
		"Bruno Mars - Uptown Funk",
		"Billie Eilish - Bad Guy",
		"Olivia Rodrigo - Drivers License",
		"Harry Styles - As It Was",
		"Glass Animals - Heat Waves",
		"Twenty One Pilots - Stressed Out",
		"OneRepublic - Counting Stars",
		"Maroon 5 - Sugar",
		"Shawn Mendes - Senorita",
		"Camila Cabello - Havana",
		"Taylor Swift - Blank Space",
		"Katy Perry - Roar",
		"Shakira - Waka Waka",
		"Luis Fonsi - Despacito",
		"Arijit Singh - Kesariya",
		"AP Dhillon - Brown Munde",
		"Sidhu Moose Wala - 295",
		"Diljit Dosanjh - Lover",
		"Badshah - Genda Phool",
		"Sub Urban - Cradles",
		"Vicetone - Astronomia",
		"https://www.youtube.com/watch?v=kXYiU_JCYtU",
	}

	destDir := "test_50tracks_dl"
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(destDir)

	type result struct {
		query       string
		title       string
		id          string
		durationSec int
		resolveMs   int64
		serverMs    float64
		downloadSec float64
		sizeMB      float64
		speedMBs    float64
		totalSec    float64
	}

	var results []result
	var failedCount int

	for i, q := range tracks {
		destFile := fmt.Sprintf("%s/track_%d.m4a", destDir, i)
		_ = os.Remove(destFile)

		t0 := time.Now()
		res, err := client.Resolve(context.Background(), q)
		resolveDur := time.Since(t0)
		if err != nil {
			t.Logf("[%02d/50] ❌ Resolve failed for '%s': %v", i+1, q, err)
			failedCount++
			continue
		}
		res = cleanTrackStreams(res)

		t1 := time.Now()
		dlRes, err := client.DownloadAudio(context.Background(), res, destFile, typetube.DownloadConfig{
			Workers:        16,
			ChunkSizeBytes: 2 * 1024 * 1024,
			Quality:        "128kbps",
		})
		downloadDur := time.Since(t1)
		_ = os.Remove(destFile)

		if err != nil {
			t.Logf("[%02d/50] ❌ Download failed for '%s' (ID: %s): %v", i+1, q, res.ID, err)
			failedCount++
			continue
		}

		totalDur := time.Since(t0)
		speed := 0.0
		if downloadDur.Seconds() > 0 {
			speed = dlRes.FileSizeMB / downloadDur.Seconds()
		}

		r := result{
			query:       q,
			title:       res.Title,
			id:          res.ID,
			durationSec: res.DurationSeconds,
			resolveMs:   resolveDur.Milliseconds(),
			serverMs:    res.LatencyMs,
			downloadSec: downloadDur.Seconds(),
			sizeMB:      dlRes.FileSizeMB,
			speedMBs:    speed,
			totalSec:    totalDur.Seconds(),
		}
		results = append(results, r)

		t.Logf("[%02d/50] %-30s | ID: %-11s | Res: %4dms (srv: %5.1fms) | DL: %4.2fs (%4.2fMB @ %4.2fMB/s) | Total: %4.2fs",
			i+1, truncateStr(q, 30), r.id, r.resolveMs, r.serverMs, r.downloadSec, r.sizeMB, r.speedMBs, r.totalSec)
	}

	var totResolveMs int64
	var totDlSec float64
	var totSizeMB float64
	var totTotalSec float64
	minTotal := 999.0
	maxTotal := 0.0

	for _, r := range results {
		totResolveMs += r.resolveMs
		totDlSec += r.downloadSec
		totSizeMB += r.sizeMB
		totTotalSec += r.totalSec
		if r.totalSec < minTotal {
			minTotal = r.totalSec
		}
		if r.totalSec > maxTotal {
			maxTotal = r.totalSec
		}
	}

	n := float64(len(results))
	avgResolve := float64(totResolveMs) / n
	avgDl := totDlSec / n
	avgSize := totSizeMB / n
	avgSpeed := totSizeMB / totDlSec
	avgTotal := totTotalSec / n

	t.Logf("==================== 50-TRACK BENCHMARK SUMMARY ====================")
	t.Logf("Success Count           : %d / %d (%.1f%%)", len(results), len(tracks), (n/float64(len(tracks)))*100)
	t.Logf("Average Resolve Latency : %.1f ms", avgResolve)
	t.Logf("Average Download Time   : %.2f s (Avg Payload: %.2f MB)", avgDl, avgSize)
	t.Logf("Average Download Speed  : %.2f MB/s", avgSpeed)
	t.Logf("Average Total E2E Time  : %.2f s (Min: %.2fs, Max: %.2fs)", avgTotal, minTotal, maxTotal)
	t.Logf("===================================================================")
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

