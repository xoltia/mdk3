package queue

import (
	"time"
)

type NewSong struct {
	// TODO: this can be a uint64
	UserID       string
	Title        string
	SongURL      string
	ThumbnailURL string
	Duration     time.Duration
}

type QueuedSong struct {
	NewSong
	ID         int
	Slug       string
	QueuedAt   time.Time
	DequeuedAt time.Time
}

func (qs *QueuedSong) IsDequeued() bool {
	return !qs.DequeuedAt.IsZero()
}
