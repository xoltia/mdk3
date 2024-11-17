package main

import (
	"testing"
	"time"
)

func TestPreviewImage(t *testing.T) {
	testSong := queuedSong{
		song: song{
			UserID:       "441037039412969474",
			Title:        "【VTuber】パイパイ仮面でどうかしらん？【宝鐘マリン/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
			SongURL:      "https://www.youtube.com/watch?v=zbWEZDA3xZc",
			ThumbnailURL: "https://i.ytimg.com/vi/zbWEZDA3xZc/maxresdefault.jpg?v=671d25b8",
			Duration:     duration(210 * time.Second),
		},
		ID:       1,
		QueuedAt: time.Date(2024, 11, 16, 17, 31, 17, 471827472, time.FixedZone("CST", -6*60*60)),
		Version:  0,
	}

	testUpcomingSongs := make([]queuedSong, 0)
	for i := 0; i < 20; i++ {
		testUpcomingSongs = append(testUpcomingSongs, testSong)
	}

	_, err := writePreviewPoster(testSong, "juan", testUpcomingSongs)
	if err != nil {
		t.Error(err)
	}
}

func TestLoadingImage(t *testing.T) {
	testSong := queuedSong{
		song: song{
			UserID:       "441037039412969474",
			Title:        "【VTuber】パイパイ仮面でどうかしらん？【宝鐘マリン/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
			SongURL:      "https://www.youtube.com/watch?v=zbWEZDA3xZc",
			ThumbnailURL: "https://i.ytimg.com/vi/zbWEZDA3xZc/maxresdefault.jpg?v=671d25b8",
			Duration:     duration(210 * time.Second),
		},
		ID:       1,
		QueuedAt: time.Date(2024, 11, 16, 17, 31, 17, 471827472, time.FixedZone("CST", -6*60*60)),
		Version:  0,
	}

	_, err := writeLoadingPoster(testSong)
	if err != nil {
		t.Error(err)
	}
}
