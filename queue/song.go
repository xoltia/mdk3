package queue

import "time"

type NewSong struct {
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
