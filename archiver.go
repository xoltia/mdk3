package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

func serialize(w io.Writer, songs []queuedSong) error {
	return json.NewEncoder(w).Encode(songs)
}

func deserialize(r io.Reader) ([]queuedSong, error) {
	var songs []queuedSong
	if err := json.NewDecoder(r).Decode(&songs); err != nil {
		return nil, err
	}
	return songs, nil
}

func recoverArchive(path string) ([]queuedSong, error) {
	f, err := os.Open(filepath.Join(path, "queue"))
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, err
	}
	defer f.Close()
	return deserialize(f)
}

type archivedSong struct {
	id      int
	version int
}

type archiver struct {
	queue      *queue
	archived   []archivedSong
	path       string
	serializer func(io.Writer, []queuedSong) error
	ticker     *time.Ticker
}

func newArchiver(q *queue, updateInterval time.Duration) *archiver {
	return &archiver{
		queue:      q,
		ticker:     time.NewTicker(updateInterval),
		serializer: serialize,
	}
}

func (a *archiver) run() {
	for range a.ticker.C {
		err := a.maybeArchive()
		if err != nil {
			log.Printf("failed to archive queue: %s", err)
		}
	}
}

func (a *archiver) maybeArchive() (err error) {
	a.queue.mu.RLock()
	defer a.queue.mu.RUnlock()
	if a.checkChanged() {
		err = a.writeArchive()
	}
	return
}

func (a *archiver) checkChanged() bool {
	if len(a.queue.songs) != len(a.archived) {
		return true
	}
	for i, s := range a.queue.songs {
		if s.ID != a.archived[i].id || s.Version != a.archived[i].version {
			return true
		}
	}
	return false
}

func (a *archiver) writeArchive() error {
	tempFile, err := os.CreateTemp(a.path, "queue-*.tmp")
	if err != nil {
		return err
	}

	if err := a.serializer(tempFile, a.queue.songs); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	filename := filepath.Join(a.path, "queue")
	if err := os.Rename(tempFile.Name(), filename); err != nil {
		return err
	}

	a.archived = a.archived[:0]
	for _, s := range a.queue.songs {
		a.archived = append(a.archived, archivedSong{s.ID, s.Version})
	}

	return nil
}

func (a *archiver) stop() {
	a.ticker.Stop()
	a.maybeArchive()
}
