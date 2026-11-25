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

// Users represents a user document in the database.
type Users struct {
	ID      int64 `bson:"_id"`
	Blocked bool  `bson:"blocked,omitempty"`
	Deleted bool  `bson:"deleted,omitempty"`
}

// AddUser adds a new user to the database if they do not already exist.
// If the user was previously flagged as having blocked the bot or having a
// deleted account, that flag is cleared here — reaching this handler at all
// (e.g. via /start) means they're reachable again.
func (db *Database) AddUser(userID int64) error {
	key := toKey(userID)
	if cached, ok := db.userCache.Get(key); ok && !cached.Blocked && !cached.Deleted {
		return nil
	}

	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.userDB.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{
			"$set":         bson.M{"blocked": false, "deleted": false},
			"$setOnInsert": bson.M{"_id": userID},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return err
	}

	db.userCache.Set(key, &Users{ID: userID})
	return nil
}

// setUserFlag sets a single boolean flag (blocked/deleted) on a user document
// and refreshes the cache. Used by the broadcast worker to mark targets that
// are no longer reachable, so future broadcasts and stats can skip/count them
// without needing to re-send to find out.
func (db *Database) setUserFlag(userID int64, field string, value bool) error {
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.userDB.UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{field: value}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return err
	}

	key := toKey(userID)
	cached, ok := db.userCache.Get(key)
	if !ok || cached == nil {
		cached = &Users{ID: userID}
	}
	updated := *cached
	switch field {
	case "blocked":
		updated.Blocked = value
	case "deleted":
		updated.Deleted = value
	}
	db.userCache.Set(key, &updated)
	return nil
}

// MarkUserBlocked flags a user as having blocked the bot.
func (db *Database) MarkUserBlocked(userID int64) error {
	return db.setUserFlag(userID, "blocked", true)
}

// MarkUserDeleted flags a user as having a deleted Telegram account.
func (db *Database) MarkUserDeleted(userID int64) error {
	return db.setUserFlag(userID, "deleted", true)
}

// RemoveUser removes a user from the database and cache.
func (db *Database) RemoveUser(userID int64) error {
	ctx, cancel := db.ctx()
	defer cancel()

	_, err := db.userDB.DeleteOne(ctx, bson.M{"_id": userID})
	if err != nil {
		return err
	}

	db.userCache.Delete(toKey(userID))
	return nil
}

// IsUserExist checks if a user exists in the database.
func (db *Database) IsUserExist(userID int64) (bool, error) {
	key := toKey(userID)
	if _, ok := db.userCache.Get(key); ok {
		return true, nil
	}

	ctx, cancel := db.ctx()
	defer cancel()

	var user Users
	err := db.userDB.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	db.userCache.Set(key, &user)
	return true, nil
}

// GetAllUsers retrieves a list of all user IDs from the database.
func (db *Database) GetAllUsers() ([]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := db.userDB.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer func(cursor *mongo.Cursor, ctx context.Context) {
		_ = cursor.Close(ctx)
	}(cursor, ctx)

	var users []int64
	for cursor.Next(ctx) {
		var doc Users
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		users = append(users, doc.ID)
		db.userCache.Set(toKey(doc.ID), &doc)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// GetActiveUsers retrieves user IDs excluding anyone flagged as having
// blocked the bot or deleted their account. Broadcasts should use this
// instead of GetAllUsers so they don't waste time/flood-wait budget on
// targets that are already known to be unreachable.
func (db *Database) GetActiveUsers() ([]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"blocked": bson.M{"$ne": true}, "deleted": bson.M{"$ne": true}}
	cursor, err := db.userDB.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer func(cursor *mongo.Cursor, ctx context.Context) {
		_ = cursor.Close(ctx)
	}(cursor, ctx)

	var users []int64
	for cursor.Next(ctx) {
		var doc Users
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		users = append(users, doc.ID)
		db.userCache.Set(toKey(doc.ID), &doc)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// UserCounts summarizes the users collection for the stats screen.
type UserCounts struct {
	Total   int64
	Active  int64
	Blocked int64
	Deleted int64
}

// GetUserCounts computes total/active/blocked/deleted user counts directly
// in MongoDB, without pulling every document into memory.
func (db *Database) GetUserCounts() (*UserCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	total, err := db.userDB.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, err
	}

	blocked, err := db.userDB.CountDocuments(ctx, bson.M{"blocked": true})
	if err != nil {
		return nil, err
	}

	deleted, err := db.userDB.CountDocuments(ctx, bson.M{"deleted": true})
	if err != nil {
		return nil, err
	}

	active, err := db.userDB.CountDocuments(ctx, bson.M{"blocked": bson.M{"$ne": true}, "deleted": bson.M{"$ne": true}})
	if err != nil {
		return nil, err
	}

	return &UserCounts{Total: total, Active: active, Blocked: blocked, Deleted: deleted}, nil
}
