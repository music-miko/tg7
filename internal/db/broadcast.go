/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package db

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// broadcastStateID is the fixed _id of the single "current broadcast" document.
const broadcastStateID = "current"

// bulkMarkChunk bounds how many IDs go into one $in filter.
const bulkMarkChunk = 5000

// BroadcastState is the persisted progress of the most recent broadcast, so an
// interrupted run (stopped with /stop_broadcast, or cut off by a restart) can
// be continued with /broadcast_resume instead of starting over.
//
// Targets are processed in ascending ID order and Watermark is the highest ID
// such that it and everything before it has been handled, so resuming is just
// "everything after Watermark", which stays correct even if the chat/user lists
// changed in between.
type BroadcastState struct {
	ID string `bson:"_id"`

	// The message being broadcast.
	SourceChatID    int64 `bson:"source_chat_id"`
	SourceMessageID int64 `bson:"source_message_id"`

	Mode string  `bson:"mode"` // "chat", "user" or "both"
	Copy bool    `bson:"copy"`
	Rate float64 `bson:"rate"`

	// Total is the number of targets when the broadcast first started.
	Total        int   `bson:"total"`
	Processed    int   `bson:"processed"`
	Watermark    int64 `bson:"watermark"`
	HasWatermark bool  `bson:"has_watermark"`

	SentChats int64 `bson:"sent_chats"`
	SentUsers int64 `bson:"sent_users"`
	Dead      int64 `bson:"dead"`
	Failed    int64 `bson:"failed"`

	// Finished is true once every target was handled.
	Finished  bool      `bson:"finished"`
	StartedAt time.Time `bson:"started_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// SaveBroadcastState stores (replaces) the current broadcast's progress.
func (db *Database) SaveBroadcastState(s *BroadcastState) error {
	ctx, cancel := db.ctx()
	defer cancel()

	s.ID = broadcastStateID
	s.UpdatedAt = time.Now()
	_, err := db.broadcastDB.ReplaceOne(ctx, bson.M{"_id": broadcastStateID}, s, options.Replace().SetUpsert(true))
	return err
}

// GetBroadcastState returns the stored broadcast progress, or nil if there is none.
func (db *Database) GetBroadcastState() (*BroadcastState, error) {
	ctx, cancel := db.ctx()
	defer cancel()

	var s BroadcastState
	err := db.broadcastDB.FindOne(ctx, bson.M{"_id": broadcastStateID}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MarkChatsInvalid flags many chats as unreachable in one round trip per
// chunk, instead of one database write per failed send.
func (db *Database) MarkChatsInvalid(ids []int64) error {
	return db.bulkSetFlag(db.chatDB, ids, "invalid", func(id int64) { db.chatCache.Delete(toKey(id)) })
}

// MarkUsersBlocked flags many users as having blocked the bot.
func (db *Database) MarkUsersBlocked(ids []int64) error {
	return db.bulkSetFlag(db.userDB, ids, "blocked", func(id int64) { db.userCache.Delete(toKey(id)) })
}

// MarkUsersDeleted flags many users as having a deleted account.
func (db *Database) MarkUsersDeleted(ids []int64) error {
	return db.bulkSetFlag(db.userDB, ids, "deleted", func(id int64) { db.userCache.Delete(toKey(id)) })
}

// bulkSetFlag sets field=true on every document whose _id is in ids, then
// drops the affected cache entries so the next read sees the new flag.
func (db *Database) bulkSetFlag(coll *mongo.Collection, ids []int64, field string, evict func(id int64)) error {
	for start := 0; start < len(ids); start += bulkMarkChunk {
		end := min(start+bulkMarkChunk, len(ids))
		chunk := ids[start:end]

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := coll.UpdateMany(ctx,
			bson.M{"_id": bson.M{"$in": chunk}},
			bson.M{"$set": bson.M{field: true}},
		)
		cancel()
		if err != nil {
			return err
		}
		for _, id := range chunk {
			evict(id)
		}
	}
	return nil
}
