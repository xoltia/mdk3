package queue

import (
	"errors"

	"github.com/dgraph-io/badger/v4"
)

type UserStats struct {
	QueuedCount   uint16
	DequeuedCount uint16
	DeletedCount  uint16
}

var ErrUserStatsNotFound = errors.New("no user stats found")

func (qtx *QueueTx) GetUserStats(userID string) (stats UserStats, err error) {
	key := userRecordKey(userID)
	stats, err = qtx.userStats(key)
	return
}

func (qtx *QueueTx) userStats(key []byte) (stats UserStats, err error) {
	err = qtx.getUnmarshaledValue(key, &stats)
	if err == badger.ErrKeyNotFound {
		err = ErrUserStatsNotFound
	}
	return
}

func (qtx *QueueTx) updateUserStats(userID string, f func(*UserStats)) (err error) {
	key := userRecordKey(userID)
	stats, err := qtx.userStats(key)
	if err == ErrUserStatsNotFound {
		err = nil
	}
	f(&stats)
	err = qtx.setMarshaledValue(key, stats)
	return
}

func userRecordKey(userID string) (k []byte) {
	k = make([]byte, 0, len(userID)+1)
	k = append(k, byte(recordTypeUserStats))
	k = append(k, userID...)
	return
}
