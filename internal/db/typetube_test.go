package db

import (
	"errors"
	"testing"
	"time"
)

func TestTypeTubeMetricsNilSafe(t *testing.T) {
	// Ensure that calls on nil or uninitialized DB do not panic
	var nilDB *Database
	nilDB.RecordTypeTubeResolveSuccess("test", 100*time.Millisecond)
	nilDB.RecordTypeTubeResolveFailure("test", errors.New("err"))
	nilDB.RecordTypeTubeDownloadSuccess("test_id", 200*time.Millisecond, 1024)
	nilDB.RecordTypeTubeDownloadFailure("test_id", errors.New("err"))

	stats, err := nilDB.GetTypeTubeStats()
	if err != nil {
		t.Fatalf("expected nil error on nilDB, got: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats struct")
	}

	failures, err := nilDB.GetRecentTypeTubeFailures(5)
	if err != nil {
		t.Fatalf("expected nil error on nilDB, got: %v", err)
	}
	if failures != nil {
		t.Fatalf("expected nil failures on nilDB, got: %v", failures)
	}

	if err := nilDB.ResetTypeTubeStats(); err != nil {
		t.Fatalf("expected nil error on nilDB, got: %v", err)
	}
}
