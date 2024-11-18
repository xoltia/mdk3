package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

type duration time.Duration

func (d *duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = duration(v)
	return nil
}

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

type song struct {
	UserID       string   `json:"user_id"`
	Title        string   `json:"title"`
	SongURL      string   `json:"song_url"`
	ThumbnailURL string   `json:"thumbnail_url"`
	Duration     duration `json:"duration"`
}

type queuedSong struct {
	song
	ID       int       `json:"id"`
	Slug     string    `json:"slug"`
	QueuedAt time.Time `json:"queued_at"`
	// Version is incremented every time the song is updated.
	// Does not need to be set manually.
	Version int `json:"version"`
}

// queue is a thread-safe queue of songs.
// Locks must be manually acquired before calling any methods,
// except for update and remove.
type queue struct {
	mu                  sync.RWMutex
	id                  int
	cond                *sync.Cond
	songs               []queuedSong
	slugs               *slugGenerator
	duration            time.Duration
	lastDequeueTime     time.Time
	lastDequeueDuration time.Duration
}

func newQueue() *queue {
	q := &queue{}
	q.cond = sync.NewCond(&q.mu)
	q.slugs = newSlugGenerator()
	return q
}

func (q *queue) restore(songs []queuedSong) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.songs = songs
	for _, song := range songs {
		if song.ID > q.id {
			q.id = song.ID
		}

		parts := strings.SplitN(song.Slug, "-", 2)
		if len(parts) == 2 {
			id, err := strconv.Atoi(parts[1])
			if err != nil {
				panic(err)
			}
			max := q.slugs.max[parts[0]]
			if id > max {
				q.slugs.max[parts[0]] = id
			}
		}
		q.slugs.count[parts[0]]++
		q.duration += time.Duration(song.Duration)
	}
}

// enqueue adds a song to the queue. Needs write lock.
func (q *queue) enqueue(s song) (queueEntry queuedSong) {
	queueEntry = queuedSong{
		song:     s,
		ID:       q.id,
		QueuedAt: time.Now(),
		Version:  0,
		Slug:     q.slugs.next(),
	}
	q.id++
	q.duration += time.Duration(s.Duration)
	q.songs = append(q.songs, queueEntry)
	q.cond.Signal()
	return
}

// dequeue removes and returns the first song in the queue. Needs write lock.
func (q *queue) dequeue(ctx context.Context) (s queuedSong, err error) {
	stop := context.AfterFunc(ctx, q.cond.Broadcast)
	defer stop()

	for len(q.songs) == 0 {
		q.cond.Wait()
		// Check if the context was canceled while waiting.
		if ctx.Err() != nil {
			err = ctx.Err()
			return
		}
	}

	s = q.songs[0]
	q.duration -= time.Duration(s.Duration)
	q.slugs.decrement(s.Slug)
	q.songs = q.songs[1:]
	q.lastDequeueTime = time.Now()
	q.lastDequeueDuration = time.Duration(s.Duration)
	return
}

// peek returns the first n songs in the queue. Needs read lock.
func (q *queue) peek(n int) []queuedSong {
	if n < 0 {
		panic("negative n")
	}
	if n > len(q.songs) {
		n = len(q.songs)
	}
	return q.songs[:n]
}

// len returns the number of songs in the queue. Needs read lock.
func (q *queue) len() int {
	return len(q.songs)
}

func (q *queue) findIndexBySlug(slug string) (i int) {
	for i = range q.songs {
		if q.songs[i].Slug == slug {
			return
		}
	}
	return -1
}

func (q *queue) removeIndex(i int) {
	q.duration -= time.Duration(q.songs[i].Duration)
	q.slugs.decrement(q.songs[i].Slug)
	q.songs = append(q.songs[:i], q.songs[i+1:]...)
}

func (q *queue) update(i int, s song) {
	q.duration -= time.Duration(q.songs[i].Duration)
	q.songs[i].song = s
	q.duration += time.Duration(s.Duration)
	q.songs[i].Version++
}

func (q *queue) move(from, to int) {
	q.songs[from], q.songs[to] = q.songs[to], q.songs[from]
}
