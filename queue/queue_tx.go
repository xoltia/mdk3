package queue

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"strconv"
	"time"

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
	// recordTypeSlugIndex is a record type for storing the slug index.
	recordTypeSlugIndex
	// recordTypeVersion is a record type for storing the version.
	recordTypeVersion
)

const headNilID = -1

var (
	ErrSongNotFound    = errors.New("song not found")
	ErrQueueEmpty      = errors.New("queue is empty")
	ErrMoveOutOfBounds = errors.New("move out of bounds")
	ErrSongDequeued    = errors.New("song has already been dequeued")
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
	if err != nil {
		return
	}

	err = qtx.clearSlugIndex(headSong.Slug)
	if err != nil {
		return
	}

	headSong.DequeuedAt = time.Now()
	err = qtx.set(headSong.ID, headSong)
	return
}

// Peek returns the head song without touching the head pointer.
func (qtx *QueueTx) Peek() (headSong QueuedSong, err error) {
	return qtx.headSong()
}

// Remove deletes a song by ID.
func (qtx *QueueTx) Remove(id int) (err error) {
	song, err := qtx.FindByID(id)
	if err != nil {
		return
	}

	if song.DequeuedAt.IsZero() {
		err = qtx.clearSlugIndex(song.Slug)
		if err != nil {
			return
		}
	}

	head, err := qtx.headID()
	if err != nil {
		return
	}

	if head == id {
		err = qtx.updateHead(id)
		if err != nil {
			return
		}
	}

	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(id))
	if err = qtx.txn.Delete(key[:]); err != nil {
		return
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
	return qtx.set(id, oldSong)
}

// Count counts all songs.
func (qtx *QueueTx) Count() (int, error) {
	head, err := qtx.headID()
	if err != nil {
		return 0, err
	}

	if head == headNilID {
		return 0, nil
	}

	iter := qtx.songIterator()
	defer iter.Close()
	iter.seekID(head)
	return iter.count(), nil
}

// IterateFromHead iterates over all songs in the queue from the head.
func (qtx *QueueTx) IterateFromHead(f func(song QueuedSong) bool) error {
	iter := qtx.songIterator()
	defer iter.Close()
	head, err := qtx.headID()
	if err != nil {
		return err
	}
	if head == headNilID {
		return nil
	}
	iter.seekID(head)

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
	slugIndexKey := make([]byte, 1+len(slug))
	slugIndexKey[0] = byte(recordTypeSlugIndex)
	copy(slugIndexKey[1:], []byte(slug))

	item, err := qtx.txn.Get(slugIndexKey)
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			err = ErrSongNotFound
		}
		return
	}

	err = item.Value(func(val []byte) error {
		id := int(binary.BigEndian.Uint64(val))
		song, err = qtx.FindByID(id)
		return err
	})
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
		return nil, err
	}
	if head == headNilID {
		return nil, nil
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

// Move changes the position of a song by ID. Only songs that
// have not yet been dequeued can be moved. Songs cannot be moved
// below the head of the queue.
func (qtx *QueueTx) Move(id, newPosition int) error {
	currentPosition, err := qtx.distanceFromHeadByID(id)
	if err != nil {
		return err
	}
	if currentPosition == newPosition {
		return nil
	}
	if currentPosition < 0 {
		return ErrSongDequeued
	}
	if newPosition < 0 {
		return ErrMoveOutOfBounds
	}

	idAtNewPos, err := qtx.idRelativeToHead(newPosition)
	if err != nil {
		return err
	}

	return qtx.moveWithoutBoundCheck(id, idAtNewPos, currentPosition, newPosition)
}

// putSong writes a new song to the database.
func (qtx *QueueTx) putSong(song NewSong) (id int, err error) {
	slugCandidate := randomSlug()

	seqID, err := qtx.queue.id.Next()
	if err != nil {
		return
	}

	id = int(seqID)
	slug, err := qtx.indexSlugFindNonDuplicate(slugCandidate, id)
	if err != nil {
		return
	}

	queuedSong := QueuedSong{
		NewSong: song,
		ID:      id,
		Slug:    slug,
	}

	return id, qtx.set(id, queuedSong)
}

func (qtx *QueueTx) clearSlugIndex(slugWithDedupeNumber string) error {
	slugIndexKey := make([]byte, 1+len(slugWithDedupeNumber))
	slugIndexKey[0] = byte(recordTypeSlugIndex)
	copy(slugIndexKey[1:], []byte(slugWithDedupeNumber))

	return qtx.txn.Delete(slugIndexKey)
}

func (qtx *QueueTx) setSlugID(slug string, id int) error {
	slugIndexKey := make([]byte, 1+len(slug))
	slugIndexKey[0] = byte(recordTypeSlugIndex)
	copy(slugIndexKey[1:], []byte(slug))

	idValue := [8]byte{}
	binary.BigEndian.PutUint64(idValue[:], uint64(id))

	return qtx.txn.Set(slugIndexKey, idValue[:])
}

func (qtx *QueueTx) indexSlugFindNonDuplicate(slug string, id int) (string, error) {
	slugKey := make([]byte, 1+len(slug))
	slugKey[0] = byte(recordTypeSlugIndex)
	copy(slugKey[1:], []byte(slug))

	slugIterator := qtx.txn.NewIterator(badger.IteratorOptions{
		Prefix: slugKey,
	})
	defer slugIterator.Close()
	slugIterator.Seek(slugKey)

	maxSlugDedupe := 0
	numberDuplicates := 0

	for slugIterator.Valid() {
		numberDuplicates++
		k := slugIterator.Item().Key()
		dedupeNumberSplit := bytes.IndexByte(k, '-')
		if dedupeNumberSplit == -1 {
			slugIterator.Next()
			continue
		}
		dedupeNumberAscii := k[dedupeNumberSplit+1:]
		dedupeNumber, err := strconv.Atoi(string(dedupeNumberAscii))
		if err != nil {
			return "", err
		}

		if dedupeNumber > maxSlugDedupe {
			maxSlugDedupe = dedupeNumber
		}

		slugIterator.Next()
	}

	var deduplicatedSlug string
	if numberDuplicates == 0 {
		deduplicatedSlug = slug
	} else {
		deduplicatedSlug = fmt.Sprintf("%s-%d", slug, maxSlugDedupe+1)
	}

	slugIndexKey := make([]byte, 1+len(deduplicatedSlug))
	slugIndexKey[0] = byte(recordTypeSlugIndex)
	copy(slugIndexKey[1:], []byte(deduplicatedSlug))

	err := qtx.setSlugID(deduplicatedSlug, id)
	return deduplicatedSlug, err
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
	return qtx.songIteratorWithOptions(25, false)
}

func (qtx *QueueTx) songIteratorReverse() *songIterator {
	iter := qtx.songIteratorWithOptions(25, true)
	iter.Rewind()
	return iter
}

func (qtx *QueueTx) songIteratorWithOptions(prefetch int, reverse bool) *songIterator {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = []byte{byte(recordTypeQueuedSong)}
	opts.PrefetchValues = prefetch > 0
	opts.Reverse = reverse
	iterator := qtx.txn.NewIterator(opts)
	iterator.Seek(opts.Prefix)
	return &songIterator{iterator}
}

// Method for moving songs to a new position.
// Will work by making a "hole" in the desired position, and shifting
// all songs on the side of the original position away from the hole
// towards the previous position. The song will then be inserted into the hole.
// Example, moving song 5 to position 3:
//
//	[ 1 2 3 4 5 6 7 8 9 ] // store 5 in a temporary variable
//	[ 1 2 x 3 4 6 7 8 9 ] // move 3, 4 up
//	[ 1 2 5 3 4 6 7 8 9 ] // insert 5
//
// Works in reverse as well, moving song 3 to position 5:
//
//	[ 1 2 3 4 5 6 7 8 9 ] // store 3 in a temporary variable
//	[ 1 2 4 5 x 6 7 8 9 ] // move 4, 5 down
//	[ 1 2 4 3 5 6 7 8 9 ] // insert 3

func (qtx *QueueTx) idRelativeToHead(distance int) (sid int, err error) {
	head, err := qtx.headID()
	if err != nil {
		return
	}

	if head == headNilID {
		err = ErrSongNotFound
		return
	}

	var iter *songIterator
	if distance < 0 {
		iter = qtx.songIteratorReverse()
		distance = -distance
	} else {
		iter = qtx.songIterator()
	}
	defer iter.Close()

	iter.seekID(head)
	for i := 0; i < distance; i++ {
		iter.Next()
		if !iter.Valid() {
			err = ErrMoveOutOfBounds
			return
		}
	}

	sid = iter.id()
	return
}

func (qtx *QueueTx) distanceFromHeadByID(id int) (distance int, err error) {
	head, err := qtx.headID()
	if err != nil {
		return
	}

	if head == headNilID {
		err = ErrSongNotFound
		return
	}

	it := qtx.songIterator()
	it.seekID(head)
	defer it.Close()

	for it.Valid() {
		if it.id() == id {
			return distance, nil
		}
		distance++
		it.Next()
	}

	err = ErrSongNotFound
	return
}

func (qtx *QueueTx) moveWithoutBoundCheck(id, idAtNewPos, currentPosition, newPosition int) error {
	// TODO: what the sigma is this?
	// This function barely makes sense to me and probably needs a rewrite.
	if idAtNewPos == id {
		return nil
	}

	currentSong, err := qtx.FindByID(id)
	if err != nil {
		return err
	}

	var it *songIterator
	if currentPosition < newPosition {
		it = qtx.songIteratorReverse()
	} else {
		it = qtx.songIterator()
	}

	defer it.Close()

	it.seekID(idAtNewPos)
	lastSong, err := it.song()
	if err != nil {
		return err
	}
	it.Next()

	for it.Valid() {
		nextSong, err := it.song()
		if err != nil {
			return err
		}

		lastSongWithSameID := lastSong
		lastSongWithSameID.ID = nextSong.ID

		err = qtx.set(nextSong.ID, lastSongWithSameID)
		if err != nil {
			return err
		}

		err = qtx.setSlugID(lastSongWithSameID.Slug, nextSong.ID)
		if err != nil {
			return err
		}

		lastSong = nextSong
		if nextSong.ID == id {
			break
		}
		it.Next()
	}

	currentSong.ID = idAtNewPos
	err = qtx.set(idAtNewPos, currentSong)
	if err != nil {
		return err
	}

	err = qtx.setSlugID(currentSong.Slug, idAtNewPos)
	return err
}

func (qtx *QueueTx) set(id int, song QueuedSong) error {
	buff := new(bytes.Buffer)
	err := gob.NewEncoder(buff).Encode(song)
	if err != nil {
		return fmt.Errorf("cannot encode queued song: %w", err)
	}

	key := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(key[1:], uint64(id))
	return qtx.txn.Set(key[:], buff.Bytes())
}
