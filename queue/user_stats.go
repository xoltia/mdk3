package queue

import (
	"encoding"
	"encoding/binary"
	"errors"

	"github.com/dgraph-io/badger/v4"
)

type UserStats struct {
	QueuedCount   uint16
	DequeuedCount uint16
	DeletedCount  uint16
}

func (s UserStats) MarshalBinary() (b []byte, err error) {
	b = make([]byte, 6)
	binary.BigEndian.PutUint16(b[0:2], s.QueuedCount)
	binary.BigEndian.PutUint16(b[2:4], s.DequeuedCount)
	binary.BigEndian.PutUint16(b[4:6], s.DeletedCount)
	return
}

func (s *UserStats) UnmarshalBinary(b []byte) error {
	if len(b) != 6 {
		return errors.New("invalid length")
	}
	s.QueuedCount = binary.BigEndian.Uint16(b[0:2])
	s.DequeuedCount = binary.BigEndian.Uint16(b[2:4])
	s.DeletedCount = binary.BigEndian.Uint16(b[4:6])
	return nil
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

// TODO: switch other gob encoded values to use this instead

// getUnmarshaledValue reads a value with key `k` and unmarshals into `v`
func (qtx *QueueTx) getUnmarshaledValue(k []byte, v encoding.BinaryUnmarshaler) (err error) {
	item, err := qtx.txn.Get(k)
	if err != nil {
		return
	}
	return item.Value(v.UnmarshalBinary)
}

// setMarshaledValue sets a key `k` with the marshalled result of `v`
func (qtx *QueueTx) setMarshaledValue(k []byte, v encoding.BinaryMarshaler) (err error) {
	vb, err := v.MarshalBinary()
	if err != nil {
		return err
	}
	return qtx.txn.Set(k, vb)
}
