package queue

import (
	"encoding/binary"
	"errors"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

const (
	version uint32 = 0
)

var (
	ErrVersionMismatch = errors.New("version mismatch")
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

	if v, err := checkVersion(db); err != nil {
		return nil, err
	} else if v != version {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrVersionMismatch, version, v)
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

func (q *Queue) GC() (err error) {
	err = q.db.RunValueLogGC(0.3)
	for err == nil {
		err = q.db.RunValueLogGC(0.3)
	}
	if err == badger.ErrNoRewrite {
		err = nil
	}
	return
}

func checkVersion(db *badger.DB) (v uint32, err error) {
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte{byte(recordTypeVersion)})
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			v = binary.BigEndian.Uint32(val)
			return nil
		})
	})
	// Set version if not set already (first run)
	if errors.Is(err, badger.ErrKeyNotFound) {
		err = db.Update(func(txn *badger.Txn) error {
			versionBytes := [4]byte{byte(recordTypeVersion)}
			binary.BigEndian.PutUint32(versionBytes[:], version)
			return txn.Set([]byte{byte(recordTypeVersion)}, versionBytes[:])
		})
		v = version
	}
	return
}
