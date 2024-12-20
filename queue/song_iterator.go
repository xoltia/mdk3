package queue

import (
	"encoding/binary"

	badger "github.com/dgraph-io/badger/v4"
)

type songIterator struct {
	*badger.Iterator
}

func (si *songIterator) seekMax() {
	si.Seek([]byte{byte(recordTypeQueuedSong), 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
}

func (si *songIterator) seekID(id int) {
	songKey := [9]byte{byte(recordTypeQueuedSong)}
	binary.BigEndian.PutUint64(songKey[1:], uint64(id))
	si.Seek(songKey[:])
}

func (si *songIterator) song() (song QueuedSong, err error) {
	item := si.Item()
	err = item.Value(song.UnmarshalBinary)
	return
}

func (si *songIterator) id() (id int) {
	key := si.Item().Key()
	id = int(binary.BigEndian.Uint64(key[1:]))
	return
}

func (si *songIterator) count() (count int) {
	for si.Valid() {
		count++
		si.Next()
	}
	return
}
