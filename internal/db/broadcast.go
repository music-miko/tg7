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
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// bulkMarkChunk bounds how many IDs go into one $in filter.
const bulkMarkChunk = 5000

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
