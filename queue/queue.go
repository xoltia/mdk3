package queue

import (
	badger "github.com/dgraph-io/badger/v4"
)

type recordType uint8

const (
	// recordTypeQueuedSong is a record type for QueuedSong.
	recordTypeQueuedSong recordType = iota
	// recordTypeHead is a record type for storing the ID of the head of the queue.
	recordTypeHead
	// recordTypeSequence is a record type for storing the sequence number.
	recordTypeSequence
)

type Queue struct {
	db *badger.DB
	id *badger.Sequence
}

func OpenQueue(path string) (*Queue, error) {
	var opts badger.Options
	if path == ":memory:" {
		opts = badger.DefaultOptions("").WithInMemory(true)
	} else {
		opts = badger.DefaultOptions(path)
	}
	opts.Logger = nil
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	queueSeqIDKey := []byte{byte(recordTypeSequence)}
	queueSeqIDKey = append(queueSeqIDKey, []byte("queue_id")...)

	seq, err := db.GetSequence(queueSeqIDKey, 100)
	if err != nil {
		return nil, err
	}

	return &Queue{
		db: db,
		id: seq,
	}, nil
}

func (q *Queue) Close() error {
	if err := q.id.Release(); err != nil {
		return err
	}
	return q.db.Close()
}

func (q *Queue) BeginTxn(write bool) *QueueTx {
	return &QueueTx{
		txn:   q.db.NewTransaction(write),
		queue: q,
	}
}

func (q *Queue) Iterate(f func(QueuedSong) bool) error {
	tx := q.BeginTxn(false)
	defer tx.Discard()

	return tx.IterateFromHead(f)
}
