package queue

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

const headNilID = -1

var (
	ErrSongNotFound = errors.New("song not found")
	ErrQueueEmpty   = errors.New("queue is empty")
)

type QueueTx struct {
	txn   *badger.Txn
	queue *Queue
}

// Commit will commit all changes in the current transaction.
func (qtx *QueueTx) Commit() error {
	return qtx.txn.Commit()
}

// Discard will cancel the current transaction.
func (qtx *QueueTx) Discard() {
	qtx.txn.Discard()
}

// Enqueue adds a song to the queue, returning the ID and error.
func (qtx *QueueTx) Enqueue(song NewSong) (id int, err error) {
	id, err = qtx.putSong(song)
	if err != nil {
		return
	}
	err = qtx.checkHead(id)
	return
}

// Dequeue returns the head song and moves the head to the next position.
// Returns ErrorQueueEmpty if there is no head.
func (qtx *QueueTx) Dequeue() (headSong QueuedSong, err error) {
	headSong, err = qtx.headSong()
	if err != nil {
		return
	}

	err = qtx.updateHead(headSong.ID)
	return
}

// Peek returns the head song without touching the head pointer.
func (qtx *QueueTx) Peek() (headSong QueuedSong, err error) {
	return qtx.headSong()
}

// Remove deletes a song by ID.
func (qtx *QueueTx) Remove(id int) (err error) {
	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(id))
	if err = qtx.txn.Delete(key[:]); err != nil {
		return
	}

	head, err := qtx.headID()
	if err != nil {
		return
	}

	if head == id {
		err = qtx.updateHead(id)
	}

	return
}

// Update changes a previously queued song by ID.
func (qtx *QueueTx) Update(id int, song NewSong) error {
	oldSong, err := qtx.FindByID(id)
	if err != nil {
		return err
	}

	oldSong.NewSong = song
	buff := new(bytes.Buffer)
	err = gob.NewEncoder(buff).Encode(oldSong)
	if err != nil {
		return fmt.Errorf("cannot encode queued song: %w", err)
	}

	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(id))
	return qtx.txn.Set(key[:], buff.Bytes())
}

// Count counts all songs.
func (qtx *QueueTx) Count() (int, error) {
	head, err := qtx.headID()
	if err != nil {
		if errors.Is(err, ErrQueueEmpty) {
			return 0, nil
		}
		return 0, err
	}

	iter := qtx.songIterator()
	iter.seekID(head)
	defer iter.Close()
	return iter.count(), nil
}

// Iterate iterates over all songs in the queue.
func (qtx *QueueTx) Iterate(f func(song QueuedSong) bool) error {
	iter := qtx.songIterator()
	defer iter.Close()

	for iter.Valid() {
		song, err := iter.song()
		if err != nil {
			return err
		}
		if !f(song) {
			break
		}
		iter.Next()
	}

	return nil
}

// FindBySlug returns a song by slug.
func (qtx *QueueTx) FindBySlug(slug string) (song QueuedSong, err error) {
	// TODO: Implement index for slug
	iter := qtx.songIterator()
	defer iter.Close()

	for iter.Valid() {
		song, err = iter.song()
		if err != nil {
			return
		}
		if song.Slug == slug {
			return
		}
		iter.Next()
	}

	err = ErrSongNotFound
	return
}

// FindByID returns a song by ID.
func (qtx *QueueTx) FindByID(id int) (song QueuedSong, err error) {
	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(id))
	item, err := qtx.txn.Get(key[:])
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			err = ErrSongNotFound
		}
		return
	}

	err = item.Value(func(val []byte) error {
		return gob.NewDecoder(bytes.NewReader(val)).Decode(&song)
	})
	return
}

// List returns all songs that haven't been dequeued from a given offset up to a limit.
func (qtx *QueueTx) List(offset, limit int) ([]QueuedSong, error) {
	head, err := qtx.headID()
	if err != nil {
		if errors.Is(err, ErrQueueEmpty) {
			return nil, nil
		}
		return nil, err
	}

	it := qtx.songIterator()
	it.seekID(head)
	defer it.Close()

	songs := make([]QueuedSong, 0, min(25, limit))
	for it.Valid() {
		if offset > 0 {
			offset--
			it.Next()
			continue
		}
		if limit == 0 {
			break
		}
		limit--
		song, err := it.song()
		if err != nil {
			return nil, err
		}
		songs = append(songs, song)
		it.Next()
	}

	return songs, nil
}

// Empty returns true if the queue is empty.
func (qtx *QueueTx) Empty() (bool, error) {
	head, err := qtx.headID()
	if err != nil {
		return false, err
	}
	return head == headNilID, nil
}

// putSong writes a new song to the database.
func (qtx *QueueTx) putSong(song NewSong) (id int, err error) {
	seqID, err := qtx.queue.id.Next()
	if err != nil {
		return
	}

	id = int(seqID)

	queuedSong := QueuedSong{
		NewSong: song,
		ID:      id,
		Slug:    fmt.Sprintf("song-%d", id), // TODO: generate slug
	}

	buff := new(bytes.Buffer)
	err = gob.NewEncoder(buff).Encode(queuedSong)
	if err != nil {
		err = fmt.Errorf("cannot encode queued song: %w", err)
		return
	}

	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], seqID)
	err = qtx.txn.Set(key[:], buff.Bytes())
	return
}

// headID reads the head of the queue from the database.
func (qtx *QueueTx) headID() (head int, err error) {
	item, err := qtx.txn.Get([]byte{byte(recordTypeHead)})
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return headNilID, nil
		}
		return
	}

	err = item.Value(func(val []byte) error {
		head = int(binary.BigEndian.Uint64(val))
		return nil
	})
	return
}

// writeHead writes the head of the queue to the database.
func (qtx *QueueTx) writeHead(head int) error {
	buff := [8]byte{}
	binary.BigEndian.PutUint64(buff[:], uint64(head))
	return qtx.txn.Set([]byte{byte(recordTypeHead)}, buff[:])
}

// checkHead checks if the head of the queue is set after a new
// song is enqueued. If the head is not set, it sets the head to
// the ID of the new song.
func (qtx *QueueTx) checkHead(id int) error {
	head, err := qtx.headID()
	if err != nil {
		return err
	}
	if head == headNilID {
		return qtx.writeHead(id)
	}
	return nil
}

// headSong reads the head song from the database.
func (qtx *QueueTx) headSong() (headSong QueuedSong, err error) {
	head, err := qtx.headID()
	if err != nil {
		return
	}

	if head == headNilID {
		err = ErrQueueEmpty
		return
	}

	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(head))
	item, err := qtx.txn.Get(key[:])
	if err != nil {
		return
	}

	err = item.Value(func(val []byte) error {
		return gob.NewDecoder(bytes.NewReader(val)).Decode(&headSong)
	})

	return
}

// updateHead updates the head of the queue to the next song.
func (qtx *QueueTx) updateHead(head int) error {
	iterator := qtx.songIterator()
	defer iterator.Close()

	iterator.seekID(head)
	iterator.Next()
	if !iterator.Valid() {
		return qtx.writeHead(headNilID)
	}
	nextID := iterator.id()
	return qtx.writeHead(nextID)
}

func (qtx *QueueTx) songIterator() *songIterator {
	iterator := qtx.txn.NewIterator(badger.IteratorOptions{
		Prefix: []byte{byte(recordTypeQueuedSong)},
	})
	iterator.Seek([]byte{byte(recordTypeQueuedSong)})
	return &songIterator{iterator}
}

// TODO: Implement method for moving songs to a new position.
// Will work by making a "hole" in the desired position, and shifting
// all songs on the side of the original position away from the hole
// towards the previous position. The song will then be inserted into the hole.
// Example, moving song 5 to position 3:
//  [ 1 2 3 4 5 6 7 8 9 ] // store 5 in a temporary variable
//  [ 1 2 x 3 4 6 7 8 9 ] // move 3, 4 up
//  [ 1 2 5 3 4 6 7 8 9 ] // insert 5
// Works in reverse as well, moving song 3 to position 5:
//  [ 1 2 3 4 5 6 7 8 9 ] // store 3 in a temporary variable
//  [ 1 2 4 5 x 6 7 8 9 ] // move 4, 5 down
//  [ 1 2 4 3 5 6 7 8 9 ] // insert 3
