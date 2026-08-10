/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package db

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// insertBatchSize caps how many documents RestoreFromZip buffers before
// issuing an InsertMany, so a very large collection doesn't need to be held
// in memory all at once.
const insertBatchSize = 500

// CollectionBackupResult reports what happened for a single collection
// during backup or restore.
type CollectionBackupResult struct {
	Collection string
	Documents  int
	Err        error
}

// BackupResult is the overall outcome of a backup or restore run.
type BackupResult struct {
	Path        string
	Collections []CollectionBackupResult
	StartedAt   time.Time
	Finished    time.Time
}

// TotalDocuments sums documents across all collections in the result.
func (r *BackupResult) TotalDocuments() int {
	total := 0
	for _, c := range r.Collections {
		total += c.Documents
	}
	return total
}

// HasErrors reports whether any collection failed.
func (r *BackupResult) HasErrors() bool {
	for _, c := range r.Collections {
		if c.Err != nil {
			return true
		}
	}
	return false
}

// BackupToZip dumps every collection in the database to a zip file under
// os.TempDir(), one newline-delimited-extended-JSON (".jsonl") entry per
// collection, and returns the path to the zip along with a per-collection
// report. The caller is responsible for removing the returned file once it
// has been sent/uploaded.
//
// Extended JSON (bson.MarshalExtJSON) is used instead of plain JSON so that
// BSON-specific types (int64 chat IDs, timestamps, etc.) round-trip exactly
// through RestoreFromZip instead of degrading to floats/strings.
func (db *Database) BackupToZip() (*BackupResult, error) {
	result := &BackupResult{StartedAt: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	names, err := db.DB.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	zipPath := fmt.Sprintf("%s/mongo_backup_%s.zip", os.TempDir(), time.Now().UTC().Format("20060102_150405"))
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { _ = zipFile.Close() }()

	zw := zip.NewWriter(zipFile)

	for _, name := range names {
		count, err := db.backupOneCollection(ctx, zw, name)
		result.Collections = append(result.Collections, CollectionBackupResult{
			Collection: name,
			Documents:  count,
			Err:        err,
		})
		if err != nil {
			slog.Error("[Backup] failed to back up collection", "collection", name, "error", err)
		}
	}

	if err := zw.Close(); err != nil {
		return result, fmt.Errorf("failed to finalize backup zip: %w", err)
	}

	result.Path = zipPath
	result.Finished = time.Now()
	return result, nil
}

// backupOneCollection streams a single collection's documents into the zip
// as extended-JSON lines and returns how many documents were written.
func (db *Database) backupOneCollection(ctx context.Context, zw *zip.Writer, name string) (int, error) {
	w, err := zw.Create(name + ".jsonl")
	if err != nil {
		return 0, err
	}

	cursor, err := db.DB.Collection(name).Find(ctx, bson.D{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	count := 0
	bw := bufio.NewWriter(w)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return count, fmt.Errorf("decode failed: %w", err)
		}

		line, err := bson.MarshalExtJSON(doc, false, false)
		if err != nil {
			return count, fmt.Errorf("marshal failed: %w", err)
		}

		if _, err := bw.Write(line); err != nil {
			return count, err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return count, err
		}
		count++
	}
	if err := cursor.Err(); err != nil {
		return count, err
	}

	return count, bw.Flush()
}

// RestoreFromZip restores collections from a zip previously produced by
// BackupToZip. For every ".jsonl" entry found, the matching collection is
// dropped and repopulated from the file's contents.
//
// This intentionally restores the exact state captured at backup time
// (drop + reinsert) rather than merging, so the result matches what /restore
// callers expect when recovering from data loss or corruption. Indexes are
// not part of this backup format and are not restored; recreate them
// separately if a collection relied on non-default indexes (e.g. uniqueness
// constraints) beyond Mongo's default _id index.
func (db *Database) RestoreFromZip(zipPath string) (*BackupResult, error) {
	result := &BackupResult{StartedAt: time.Now(), Path: zipPath}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup zip: %w", err)
	}
	defer func() { _ = zr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	found := false
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".jsonl") {
			continue
		}
		found = true

		name := strings.TrimSuffix(f.Name, ".jsonl")
		count, err := db.restoreOneCollection(ctx, f, name)
		result.Collections = append(result.Collections, CollectionBackupResult{
			Collection: name,
			Documents:  count,
			Err:        err,
		})
		if err != nil {
			slog.Error("[Restore] failed to restore collection", "collection", name, "error", err)
		}
	}

	if !found {
		return nil, fmt.Errorf("no .jsonl collection entries found in this zip - is it a backup produced by /backup?")
	}

	result.Finished = time.Now()
	return result, nil
}

// restoreOneCollection drops and repopulates a single collection from one
// ".jsonl" zip entry.
func (db *Database) restoreOneCollection(ctx context.Context, f *zip.File, name string) (int, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()

	coll := db.DB.Collection(name)
	if err := coll.Drop(ctx); err != nil {
		return 0, fmt.Errorf("failed to drop existing collection before restore: %w", err)
	}

	count := 0
	batch := make([]any, 0, insertBatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := coll.InsertMany(ctx, batch); err != nil {
			return err
		}
		count += len(batch)
		batch = batch[:0]
		return nil
	}

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var doc bson.M
		if err := bson.UnmarshalExtJSON(line, false, &doc); err != nil {
			return count, fmt.Errorf("failed to parse document: %w", err)
		}
		batch = append(batch, doc)

		if len(batch) >= insertBatchSize {
			if err := flush(); err != nil {
				return count, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	if err := flush(); err != nil {
		return count, err
	}

	return count, nil
}
