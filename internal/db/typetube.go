/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package db

import (
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TypeTubeStats holds aggregated resolution and download metrics.
type TypeTubeStats struct {
	ID                 string    `bson:"_id" json:"id"`
	ResolutionsSuccess int64     `bson:"resolutions_success" json:"resolutions_success"`
	ResolutionsFailure int64     `bson:"resolutions_failure" json:"resolutions_failure"`
	DownloadsSuccess   int64     `bson:"downloads_success" json:"downloads_success"`
	DownloadsFailure   int64     `bson:"downloads_failure" json:"downloads_failure"`
	LastResolveAt      time.Time `bson:"last_resolve_at,omitempty" json:"last_resolve_at,omitempty"`
	LastResolveQuery   string    `bson:"last_resolve_query,omitempty" json:"last_resolve_query,omitempty"`
	LastResolveDurMs   int64     `bson:"last_resolve_dur_ms,omitempty" json:"last_resolve_dur_ms,omitempty"`
	LastDownloadAt     time.Time `bson:"last_download_at,omitempty" json:"last_download_at,omitempty"`
	LastDownloadTarget string    `bson:"last_download_target,omitempty" json:"last_download_target,omitempty"`
	LastDownloadDurMs  int64     `bson:"last_download_dur_ms,omitempty" json:"last_download_dur_ms,omitempty"`
	LastDownloadBytes  int64     `bson:"last_download_bytes,omitempty" json:"last_download_bytes,omitempty"`
	LastResolveFailAt  time.Time `bson:"last_resolve_fail_at,omitempty" json:"last_resolve_fail_at,omitempty"`
	LastResolveFailQ   string    `bson:"last_resolve_fail_q,omitempty" json:"last_resolve_fail_q,omitempty"`
	LastResolveFailErr string    `bson:"last_resolve_fail_err,omitempty" json:"last_resolve_fail_err,omitempty"`
	LastDownloadFailAt time.Time `bson:"last_download_fail_at,omitempty" json:"last_download_fail_at,omitempty"`
	LastDownloadFailT  string    `bson:"last_download_fail_t,omitempty" json:"last_download_fail_t,omitempty"`
	LastDownloadFailErr string   `bson:"last_download_fail_err,omitempty" json:"last_download_fail_err,omitempty"`
	UpdatedAt          time.Time `bson:"updated_at" json:"updated_at"`
}

// TypeTubeFailureEvent represents a single logged failure event in DB.
type TypeTubeFailureEvent struct {
	Timestamp time.Time `bson:"timestamp" json:"timestamp"`
	Kind      string    `bson:"kind" json:"kind"` // "resolve" or "download"
	Target    string    `bson:"target" json:"target"`
	Error     string    `bson:"error" json:"error"`
}

const typeTubeGlobalDocID = "global"

// RecordTypeTubeResolveSuccess logs a successful resolution to DB.
func (db *Database) RecordTypeTubeResolveSuccess(query string, dur time.Duration) {
	if db == nil || db.typeTubeStatsDB == nil {
		return
	}
	ctx, cancel := db.ctx()
	defer cancel()

	now := time.Now()
	_, err := db.typeTubeStatsDB.UpdateOne(ctx,
		bson.M{"_id": typeTubeGlobalDocID},
		bson.M{
			"$inc": bson.M{"resolutions_success": 1},
			"$set": bson.M{
				"last_resolve_at":     now,
				"last_resolve_query":  query,
				"last_resolve_dur_ms": dur.Milliseconds(),
				"updated_at":          now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		slog.Warn("[DB] Failed to record TypeTube resolve success", "error", err)
	}
}

// RecordTypeTubeResolveFailure logs a resolution failure to DB and inserts into failure log.
func (db *Database) RecordTypeTubeResolveFailure(query string, rErr error) {
	if db == nil || db.typeTubeStatsDB == nil {
		return
	}
	ctx, cancel := db.ctx()
	defer cancel()

	now := time.Now()
	errStr := ""
	if rErr != nil {
		errStr = rErr.Error()
	}

	_, err := db.typeTubeStatsDB.UpdateOne(ctx,
		bson.M{"_id": typeTubeGlobalDocID},
		bson.M{
			"$inc": bson.M{"resolutions_failure": 1},
			"$set": bson.M{
				"last_resolve_fail_at":  now,
				"last_resolve_fail_q":   query,
				"last_resolve_fail_err": errStr,
				"updated_at":            now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		slog.Warn("[DB] Failed to record TypeTube resolve failure", "error", err)
	}

	// Insert into failure events collection
	if db.typeTubeFailuresDB != nil {
		_, _ = db.typeTubeFailuresDB.InsertOne(ctx, TypeTubeFailureEvent{
			Timestamp: now,
			Kind:      "resolve",
			Target:    query,
			Error:     errStr,
		})
	}
}

// RecordTypeTubeDownloadSuccess logs a successful download to DB.
func (db *Database) RecordTypeTubeDownloadSuccess(target string, dur time.Duration, sizeBytes int64) {
	if db == nil || db.typeTubeStatsDB == nil {
		return
	}
	ctx, cancel := db.ctx()
	defer cancel()

	now := time.Now()
	_, err := db.typeTubeStatsDB.UpdateOne(ctx,
		bson.M{"_id": typeTubeGlobalDocID},
		bson.M{
			"$inc": bson.M{"downloads_success": 1},
			"$set": bson.M{
				"last_download_at":      now,
				"last_download_target":  target,
				"last_download_dur_ms":  dur.Milliseconds(),
				"last_download_bytes":   sizeBytes,
				"updated_at":            now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		slog.Warn("[DB] Failed to record TypeTube download success", "error", err)
	}
}

// RecordTypeTubeDownloadFailure logs a download failure to DB and inserts into failure log.
func (db *Database) RecordTypeTubeDownloadFailure(target string, dErr error) {
	if db == nil || db.typeTubeStatsDB == nil {
		return
	}
	ctx, cancel := db.ctx()
	defer cancel()

	now := time.Now()
	errStr := ""
	if dErr != nil {
		errStr = dErr.Error()
	}

	_, err := db.typeTubeStatsDB.UpdateOne(ctx,
		bson.M{"_id": typeTubeGlobalDocID},
		bson.M{
			"$inc": bson.M{"downloads_failure": 1},
			"$set": bson.M{
				"last_download_fail_at":  now,
				"last_download_fail_t":   target,
				"last_download_fail_err": errStr,
				"updated_at":             now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		slog.Warn("[DB] Failed to record TypeTube download failure", "error", err)
	}

	// Insert into failure events collection
	if db.typeTubeFailuresDB != nil {
		_, _ = db.typeTubeFailuresDB.InsertOne(ctx, TypeTubeFailureEvent{
			Timestamp: now,
			Kind:      "download",
			Target:    target,
			Error:     errStr,
		})
	}
}

// GetTypeTubeStats fetches the aggregated metrics from DB.
func (db *Database) GetTypeTubeStats() (*TypeTubeStats, error) {
	if db == nil || db.typeTubeStatsDB == nil {
		return &TypeTubeStats{}, nil
	}
	ctx, cancel := db.ctx()
	defer cancel()

	var stats TypeTubeStats
	err := db.typeTubeStatsDB.FindOne(ctx, bson.M{"_id": typeTubeGlobalDocID}).Decode(&stats)
	if err != nil {
		return &TypeTubeStats{ID: typeTubeGlobalDocID}, nil
	}
	return &stats, nil
}

// GetRecentTypeTubeFailures returns the last N failure events.
func (db *Database) GetRecentTypeTubeFailures(limit int64) ([]*TypeTubeFailureEvent, error) {
	if db == nil || db.typeTubeFailuresDB == nil {
		return nil, nil
	}
	ctx, cancel := db.ctx()
	defer cancel()

	if limit <= 0 {
		limit = 10
	}

	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(limit)
	cursor, err := db.typeTubeFailuresDB.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var failures []*TypeTubeFailureEvent
	if err := cursor.All(ctx, &failures); err != nil {
		return nil, err
	}
	return failures, nil
}

// ResetTypeTubeStats clears the statistics in DB.
func (db *Database) ResetTypeTubeStats() error {
	if db == nil || db.typeTubeStatsDB == nil {
		return nil
	}
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.typeTubeStatsDB.DeleteOne(ctx, bson.M{"_id": typeTubeGlobalDocID})
	return err
}
