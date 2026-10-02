package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/playeon/typetube-go"
)

func cleanTrackStreams(res *typetube.TrackResult) {
	if res == nil {
		return
	}
	var cleanedAudio []typetube.AudioStream
	for _, s := range res.AudioStreams {
		if !strings.Contains(s.URL, "c=WEB_CREATOR") {
			cleanedAudio = append(cleanedAudio, s)
		}
	}
	if len(cleanedAudio) > 0 {
		res.AudioStreams = cleanedAudio
	}
	var cleanedVideo []typetube.VideoStream
	for _, s := range res.VideoStreams {
		if !strings.Contains(s.URL, "c=WEB_CREATOR") {
			cleanedVideo = append(cleanedVideo, s)
		}
	}
	if len(cleanedVideo) > 0 {
		res.VideoStreams = cleanedVideo
	}
}

func main() {
	client := typetube.NewClient(
		typetube.WithAPIKey("tt_priv_9999999"),
		typetube.WithFormat("protobuf"),
		typetube.WithHost("https://typetube.xysushi.in"),
	)

	// 50 tracks confirmed/unlikely to have geo-block / country restrictions (GCR)
	tracks := []string{
		"Lost Sky - Fearless pt.II",
		"Rick Astley - Never Gonna Give You Up",
		"Alan Walker - Faded",
		"Cartoon - On & On",
		"Elektronomia - Sky High",
		"dQw4w9WgXcQ",
		"Dua Lipa - Levitating",
		"Coldplay - Viva La Vida",
		"Linkin Park - In The End",
		"Marshmello - Alone",
		"Alan Walker - The Spectre",
		"K-391 - Summertime",
		"Tobu - Hope",
		"Janji - Heroes Tonight",
		"Deorro - Five Hours",
		"Martin Garrix - Animals",
		"David Guetta - Titanium",
		"twenty one pilots - Stressed Out",
		"Ed Sheeran - Shape of You",
		"Taylor Swift - Blank Space",
		"Adele - Rolling in the Deep",
		"Sub Urban - Cradles",
		"Vicetone - Astronomia",
		"Different Heaven & EH!DE - My Heart",
		"DEAF KEV - Invincible",
		"Disfigure - Blank",
		"Spektrem - Shine",
		"Warriyo - Mortals",
		"Culture Code - Make Me Move",
		"Jim Yosef - Firefly",
		"Tobu - Candyland",
		"TheFatRat - Unity",
		"TheFatRat - Monody",
		"TheFatRat - Fly Away",
		"Alan Walker - Force",
		"Electro-Light - Symbolism",
		"RetroVision - Puzzle",
		"Unknown Brain - Superhero",
		"JJD - Adventure",
		"Aero Chord - Surface",
		"Desmeon - Hellcat",
		"K-391 - Earth",
		"Lensko - Circles",
		"Diviners - Savannah",
		"Ship Wrek & Zookeepers - Ark",
		"Itro & Tobu - Cloud 9",
		"Janji - Together",
		"Jim Yosef - Eclipse",
		"Elektronomia - Limitless",
		"Syn Cole - Feel Good",
	}

	destDir := "/tmp/test_oracle_50tracks"
	_ = os.MkdirAll(destDir, 0755)
	defer os.RemoveAll(destDir)

	type result struct {
		idx         int
		query       string
		title       string
		id          string
		resolveMs   int64
		serverMs    float64
		downloadSec float64
		sizeMB      float64
		speedMBs    float64
		totalSec    float64
		err         error
	}

	var results []result
	successCount := 0
	failedCount := 0

	fmt.Printf("================================================================================\n")
	fmt.Printf("Starting 50-Track Non-Geo-Blocked Benchmark on Oracle VPS (129.154.233.132)\n")
	fmt.Printf("Endpoint: https://typetube.xysushi.in | Workers: 16 | Format: 128kbps\n")
	fmt.Printf("================================================================================\n")

	tTotalStart := time.Now()

	for i, q := range tracks {
		destFile := fmt.Sprintf("%s/track_%d.m4a", destDir, i)
		_ = os.Remove(destFile)

		t0 := time.Now()
		res, err := client.Resolve(context.Background(), q)
		resolveDur := time.Since(t0)
		if err != nil {
			fmt.Printf("[%02d/50] [FAIL RESOLVE] %s: %v\n", i+1, q, err)
			failedCount++
			results = append(results, result{idx: i + 1, query: q, err: err})
			continue
		}

		cleanTrackStreams(res)

		t1 := time.Now()
		dlRes, err := client.DownloadAudio(context.Background(), res, destFile, typetube.DownloadConfig{
			Workers:        16,
			ChunkSizeBytes: 2 * 1024 * 1024,
			Quality:        "128kbps",
		})
		downloadDur := time.Since(t1)
		_ = os.Remove(destFile)

		if err != nil {
			fmt.Printf("[%02d/50] [FAIL DOWNLOAD] %s (ID: %s): %v\n", i+1, res.Title, res.ID, err)
			failedCount++
			results = append(results, result{idx: i + 1, query: q, title: res.Title, id: res.ID, err: err})
			continue
		}

		totalDur := time.Since(t0)
		sizeMB := dlRes.FileSizeMB
		speedMBs := 0.0
		if downloadDur.Seconds() > 0 {
			speedMBs = sizeMB / downloadDur.Seconds()
		}

		shortTitle := res.Title
		if len(shortTitle) > 30 {
			shortTitle = shortTitle[:27] + "..."
		}

		fmt.Printf("[%02d/50] %-30s | ID: %-11s | Res: %4dms (srv: %5.1fms) | DL: %.2fs (%4.2fMB @ %4.2fMB/s) | Total: %.2fs\n",
			i+1, shortTitle, res.ID,
			resolveDur.Milliseconds(), res.LatencyMs,
			downloadDur.Seconds(), sizeMB, speedMBs,
			totalDur.Seconds(),
		)

		successCount++
		results = append(results, result{
			idx:         i + 1,
			query:       q,
			title:       res.Title,
			id:          res.ID,
			resolveMs:   resolveDur.Milliseconds(),
			serverMs:    res.LatencyMs,
			downloadSec: downloadDur.Seconds(),
			sizeMB:      sizeMB,
			speedMBs:    speedMBs,
			totalSec:    totalDur.Seconds(),
		})
	}

	wallClockSec := time.Since(tTotalStart).Seconds()

	var totResolveMs int64
	var totDlSec float64
	var totSizeMB float64
	var totTotalSec float64
	minTotal := 999999.0
	maxTotal := 0.0

	for _, r := range results {
		if r.err == nil {
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
	}

	n := float64(successCount)
	avgResolve := 0.0
	avgDl := 0.0
	avgSize := 0.0
	avgSpeed := 0.0
	avgTotal := 0.0

	if n > 0 {
		avgResolve = float64(totResolveMs) / n
		avgDl = totDlSec / n
		avgSize = totSizeMB / n
		if totDlSec > 0 {
			avgSpeed = totSizeMB / totDlSec
		}
		avgTotal = totTotalSec / n
	}

	fmt.Printf("\n==================== 50-TRACK BENCHMARK SUMMARY ====================\n")
	fmt.Printf("Total Tracks Tested    : 50\n")
	fmt.Printf("Success Rate           : %d / 50 (%.1f%%)\n", successCount, float64(successCount)/50.0*100.0)
	fmt.Printf("Failed Count           : %d\n", failedCount)
	fmt.Printf("Average Resolve Latency: %.1f ms\n", avgResolve)
	fmt.Printf("Average Download Time  : %.2f s (Avg Payload: %.2f MB)\n", avgDl, avgSize)
	fmt.Printf("Average Download Speed : %.2f MB/s\n", avgSpeed)
	fmt.Printf("Average Total E2E Time : %.2f s (Min: %.2fs, Max: %.2fs)\n", avgTotal, minTotal, maxTotal)
	fmt.Printf("Total Wall Clock Time  : %.2f s\n", wallClockSec)
	fmt.Printf("===================================================================\n")
}
