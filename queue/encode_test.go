package queue_test

import (
	"testing"
	"time"

	"github.com/xoltia/mdk3/queue"
)

func TestEncoding(t *testing.T) {
	now := time.Now()

	s := queue.QueuedSong{
		NewSong: queue.NewSong{
			UserID:       "user",
			Title:        "title",
			SongURL:      "song",
			ThumbnailURL: "thumb",
			Duration:     100,
		},
		ID:         1,
		Slug:       "slug",
		QueuedAt:   now,
		DequeuedAt: now,
	}

	b, err := s.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var s2 queue.QueuedSong
	if err := s2.UnmarshalBinary(b); err != nil {
		t.Fatal(err)
	}
	if !s.QueuedAt.Equal(s2.QueuedAt) {
		t.Fatalf("expected %v, got %v", s.QueuedAt, s2.QueuedAt)
	}
	if !s.DequeuedAt.Equal(s2.DequeuedAt) {
		t.Fatalf("expected %v, got %v", s.DequeuedAt, s2.DequeuedAt)
	}
	if s.ID != s2.ID {
		t.Fatalf("expected %d, got %d", s.ID, s2.ID)
	}
	if s.Slug != s2.Slug {
		t.Fatalf("expected %s, got %s", s.Slug, s2.Slug)
	}
	if s.Duration != s2.Duration {
		t.Fatalf("expected %d, got %d", s.Duration, s2.Duration)
	}
	if s.UserID != s2.UserID {
		t.Fatalf("expected %s, got %s", s.UserID, s2.UserID)
	}
	if s.Title != s2.Title {
		t.Fatalf("expected %s, got %s", s.Title, s2.Title)
	}
	if s.SongURL != s2.SongURL {
		t.Fatalf("expected %s, got %s", s.SongURL, s2.SongURL)
	}
	if s.ThumbnailURL != s2.ThumbnailURL {
		t.Fatalf("expected %s, got %s", s.ThumbnailURL, s2.ThumbnailURL)
	}
	if s.Slug != s2.Slug {
		t.Fatalf("expected %s, got %s", s.Slug, s2.Slug)
	}

	s3 := queue.QueuedSong{}
	s3b, err := s3.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	s4 := queue.QueuedSong{}
	if err := s4.UnmarshalBinary(s3b); err != nil {
		t.Fatal(err)
	}

	if !s4.QueuedAt.IsZero() {
		t.Fatalf("expected zero, got %v", s4.QueuedAt)
	}

	if !s4.DequeuedAt.IsZero() {
		t.Fatalf("expected zero, got %v", s4.DequeuedAt)
	}
}
