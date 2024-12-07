package queue

import (
	"encoding"
	"encoding/binary"
	"errors"
	"time"
)

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

func (qs *QueuedSong) MarshalBinary() ([]byte, error) {
	size := 8                        // ID
	size += 16                       // QueuedAt
	size += 16                       // DequeuedAt
	size += 8                        // Duration
	size += 4 + len(qs.UserID)       // UserID
	size += 4 + len(qs.Title)        // Title
	size += 4 + len(qs.SongURL)      // SongURL
	size += 4 + len(qs.ThumbnailURL) // ThumbnailURL
	size += 4 + len(qs.Slug)         // Slug
	buf := make([]byte, size)
	binary.BigEndian.PutUint64(buf, uint64(qs.ID))
	if err := writeTime(buf[8:], qs.QueuedAt); err != nil {
		return nil, err
	}
	if err := writeTime(buf[24:], qs.DequeuedAt); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint64(buf[40:], uint64(qs.Duration))
	writeStrings(buf[48:], qs.UserID, qs.Title, qs.SongURL, qs.ThumbnailURL, qs.Slug)
	return buf, nil
}

func (qs *QueuedSong) UnmarshalBinary(data []byte) error {
	qs.ID = int(binary.BigEndian.Uint64(data[:8]))
	if err := qs.QueuedAt.UnmarshalBinary(timeUnmarshalSlice(data[8:])); err != nil {
		return err
	}
	if err := qs.DequeuedAt.UnmarshalBinary(timeUnmarshalSlice(data[24:])); err != nil {
		return err
	}
	qs.Duration = time.Duration(binary.BigEndian.Uint64(data[40:48]))
	readStrings(data[48:], &qs.UserID, &qs.Title, &qs.SongURL, &qs.ThumbnailURL, &qs.Slug)
	return nil
}

// Weird hack because unmarshal isn't happy when
// slice isn't exactly the right size
const (
	timeBinaryVersionV1 byte = iota + 1
	timeBinaryVersionV2
)

func timeUnmarshalSlice(data []byte) []byte {
	switch data[0] {
	case timeBinaryVersionV1:
		return data[:15]
	case timeBinaryVersionV2:
		return data[:16]
	default:
		panic("unknown time binary version")
	}
}

func writeTime(buf []byte, t time.Time) error {
	tb, err := t.MarshalBinary()
	if err != nil {
		return err
	}
	copy(buf, tb)
	return nil
}

func writeStrings(buf []byte, strings ...string) int {
	i := 0
	for _, s := range strings {
		binary.BigEndian.PutUint32(buf[i:], uint32(len(s)))
		i += 4
		i += copy(buf[i:], s)
	}
	return i
}

func readStrings(buf []byte, strings ...*string) int {
	i := 0
	for _, s := range strings {
		l := int(binary.BigEndian.Uint32(buf[i:]))
		i += 4
		*s = string(buf[i : i+l])
		i += l
	}
	return i
}
