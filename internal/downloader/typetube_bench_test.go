package downloader

import (
	"context"
	"fmt"
	"os"
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
