package queue_test

import (
	"testing"
	"time"

	"github.com/xoltia/mdk3/queue"
)

// Needed for tests that need consistent slug order
func seedRandomSlugs() {
	var seed [32]byte
	for i := range seed {
		seed[i] = 0
	}
	queue.SeedSlugGenerator(seed)
}

func TestQueueEnqueueManySongs(t *testing.T) {
	q, err := queue.OpenQueue(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	tx := q.BeginTxn(true)
	defer tx.Discard()

	count, err := tx.Count()
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	empty, err := tx.Empty()
	if err != nil {
		t.Fatal(err)
	}

	if !empty {
		t.Error("expected empty queue")
	}

	slugs := make(map[string]struct{})
	for _, song := range tests {
		sid, err := tx.Enqueue(song)
		if err != nil {
			t.Fatal(err)
		}

		song, err := tx.FindByID(sid)
		if err != nil {
			t.Fatal(err)
		}

		if _, ok := slugs[song.Slug]; ok {
			t.Errorf("slug %s is duplicated", song.Slug)
		}
		slugs[song.Slug] = struct{}{}
	}

	count, err = tx.Count()
	if err != nil {
		t.Fatal(err)
	}

	if count != len(tests) {
		t.Errorf("expected %d, got %d", len(tests), count)
	}
}

func TestQueueLastDequeued(t *testing.T) {
	q, err := queue.OpenQueue(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	tx := q.BeginTxn(true)
	defer tx.Discard()

	_, err = tx.LastDequeued()
	if err != queue.ErrSongNotFound {
		t.Fatalf("expected %v, got %v", queue.ErrSongNotFound, err)
	}

	if _, err := tx.Enqueue(tests[0]); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Dequeue(); err != nil {
		t.Fatal(err)
	}

	count, err := tx.Count()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(count)

	song, err := tx.LastDequeued()
	if err != nil {
		t.Fatal(err)
	}

	if song.NewSong != tests[0] {
		t.Fatalf("expected %v, got %v", tests[0], song)
	}

	for _, song := range tests[1:] {
		_, err := tx.Enqueue(song)
		if err != nil {
			t.Fatal(err)
		}
	}

	song, err = tx.Dequeue()
	if err != nil {
		t.Fatal(err)
	}

	t.Log(song.DequeuedAt)

	if song.DequeuedAt.IsZero() {
		t.Error("expected DequeuedAt not to be zero")
	}

	if song.NewSong != tests[1] {
		t.Errorf("expected %v, got %v", tests[1], song)
	}
}

func TestQueueDequeue(t *testing.T) {
	q, err := queue.OpenQueue(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	tx := q.BeginTxn(true)
	defer tx.Discard()

	for _, song := range tests {
		_, err := tx.Enqueue(song)
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < len(tests); i++ {
		song, err := tx.Dequeue()
		if err != nil {
			t.Fatal(err)
		}
		if song.ID != i {
			t.Errorf("expected %d, got %d", i, song.ID)
		}
	}

	empty, err := tx.Empty()
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("expected empty queue")
	}

	_, err = tx.Dequeue()
	if err != queue.ErrQueueEmpty {
		t.Errorf("expected %v, got %v", queue.ErrQueueEmpty, err)
	}

}

func TestQueueRemove(t *testing.T) {
	seedRandomSlugs()
	q, err := queue.OpenQueue(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	tx := q.BeginTxn(true)
	defer tx.Discard()

	for _, song := range tests {
		_, err := tx.Enqueue(song)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Always first because of the seed
	// Change this if the seed changes
	song, err := tx.FindBySlug("suha")
	if err != nil {
		t.Fatal(err)
	}

	if song.ID != 0 {
		t.Errorf("expected 0, got %d", song.ID)
	}

	err = tx.Remove(song.ID)
	if err != nil {
		t.Fatal(err)
	}

	song, err = tx.Dequeue()
	if err != nil {
		t.Fatal(err)
	}

	if song.ID != 1 {
		t.Errorf("expected 1, got %d", song.ID)
	}
}

func TestMoveForwardPosition(t *testing.T) {
	testMove(t, 5, 0, 10)
}

func TestMoveBackwardPosition(t *testing.T) {
	testMove(t, 5, 9, 10)
}

func TestMoveSamePosition(t *testing.T) {
	testMove(t, 5, 5, 10)
}

func testMove(t *testing.T, id, to, max int) {
	if max > len(tests) {
		panic("max is greater than the number of tests")
	}

	q, err := queue.OpenQueue(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()

	tx := q.BeginTxn(true)
	defer tx.Discard()

	slugs := []string{}
	for _, song := range tests[:max] {
		id, err := tx.Enqueue(song)
		if err != nil {
			t.Fatal(err)
		}
		s, err := tx.FindByID(id)
		if err != nil {
			t.Fatal(err)
		}

		slugs = append(slugs, s.Slug)
	}

	err = tx.Move(id, to)
	if err != nil {
		t.Fatal(err)
	}

	expectedOrder := []string{}

	// Note: only works because ID = position without any previous remove operations
	if to < id {
		expectedOrder = append(expectedOrder, slugs[:to]...)
		expectedOrder = append(expectedOrder, slugs[id])
		expectedOrder = append(expectedOrder, slugs[to:id]...)
		expectedOrder = append(expectedOrder, slugs[id+1:]...)
	} else {
		expectedOrder = append(expectedOrder, slugs[:id]...)
		expectedOrder = append(expectedOrder, slugs[id+1:to+1]...)
		expectedOrder = append(expectedOrder, slugs[id])
		expectedOrder = append(expectedOrder, slugs[to+1:]...)
	}

	t.Log(slugs)
	t.Log(expectedOrder)

	songs, err := tx.List(0, max)
	if err != nil {
		t.Fatal(err)
	}

	for i, song := range songs {
		if song.Slug != expectedOrder[i] {
			t.Errorf("expected %s, got %s", expectedOrder[i], song.Slug)
		}
	}
}

var tests = []queue.NewSong{
	{
		Title:        "【VTuber】パイパイ仮面でどうかしらん？【宝鐘マリン/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=z62VND5i86Q",
		Duration:     time.Duration(295000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/z62VND5i86Q/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAThPUiyoyOEQMqfUyL_WPgimxKVQ",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】フィーリングラデーション【ReGLOSS/hololive DEV_IS】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=qJ7xkztiQr8",
		Duration:     time.Duration(232000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/qJ7xkztiQr8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLB1LU012-TaCwgVOJ6kT8-LD38uCA",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】さくらんぼメッセージ【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=JgWTdHzmTWM",
		Duration:     time.Duration(204000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/JgWTdHzmTWM/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDswuTgZofaEvjYWLpEfQ7644g_Pw",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】今日も大天才っ！【一条莉々華/hololive DEV_IS -ReGLOSS-】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=eZhvFmpnEBY",
		Duration:     time.Duration(211000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/eZhvFmpnEBY/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBMSlFN70Au7pZGdWVLI2jv47NYDQ",
		UserID:       "1120111914764992512",
	},
	{
		Title:        "【VTuber】On your side【魔法少女ホロウィッチ！/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=26z78t_j9uU",
		Duration:     time.Duration(230000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/26z78t_j9uU/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDzAntHlo28TllxDzsZOBqhuOT0LA",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】小心旅行【大神ミオ/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=O9PW0gs9zBM",
		Duration:     time.Duration(203000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/O9PW0gs9zBM/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCGboURyLzzXXi0W5ZH6P0jnLroBg",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】Forever glow【尾丸ポルカ/ホロライブ5期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=duVABzETg3M",
		Duration:     time.Duration(223000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/duVABzETg3M/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAa15Ozrz43Q2UdYQ5FIFUr3HHATA",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】わたしを甘やかすなら【雪花ラミィ/ホロライブ5期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=s-ru7iwhVX8",
		Duration:     time.Duration(241000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/s-ru7iwhVX8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBDV_1IRaSrOExA0f9k5goB6IA-oQ",
		UserID:       "970065326605598720",
	},
	{
		Title:        "【VTuber】BAN RTA【赤井はあと/ホロライブ1期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=YhGBJjBKKO8",
		Duration:     time.Duration(200000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/YhGBJjBKKO8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAz95Pm2VnPFbLPJqqmgJB5ooh2YQ",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】マーブル冴える【不知火フレア/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=kAcFZcBM7SA",
		Duration:     time.Duration(260000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/kAcFZcBM7SA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAoKouSpjzh3dkzoJkoyRx2rimfrQ",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】泡沫メイビー【ReGLOSS/hololive DEV_IS】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=jeCePQsPLdg",
		Duration:     time.Duration(177000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/jeCePQsPLdg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBdGdYUOBGYuxx9dqFnWgVPNaaUlQ",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】Born to be “BAU”DOL☆★【FUWAMOCO/ホロライブEN Advent】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=TTOayidJLkE",
		Duration:     time.Duration(228000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/TTOayidJLkE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAgzhjzgZSWb8ezFeyhDv9ye6uhng",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】BLUE CLAPPER (MVバージョン)【hololive IDOL PROJECT/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=x1-6PGSzDio",
		Duration:     time.Duration(262000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/x1-6PGSzDio/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAqPxyCMI73aITx5ookJZoEtE1Epg",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】イケ贄【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=0b2h4tJ-dcw",
		Duration:     time.Duration(254000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/0b2h4tJ-dcw/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAsYFhZEIwfC_0DcOQloy7k9DuIHw",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】幽霊船戦【宝鐘マリン/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=J71kJSLYKQ0",
		Duration:     time.Duration(283000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/J71kJSLYKQ0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBr4BMJBdodFy9KBmkyOt1A7SBVAg",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】DAIDAIDAIファンタジスタ【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=j-3qigIAI1E",
		Duration:     time.Duration(234000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/j-3qigIAI1E/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAn2udV4CjGE7vuWmMHH7iFBBxVPA",
		UserID:       "970065326605598720",
	},
	{
		Title:        "【VTuber】Dreamy Sky【風真いろは/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=IIVu1OUFV1E",
		Duration:     time.Duration(228000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/IIVu1OUFV1E/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDMEah0VIwl5F4vcKrZdVaUevatYw",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】君と眺める夏の花【夏色まつり/ホロライブ1期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=N6xNuuZTonw",
		Duration:     time.Duration(212000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/N6xNuuZTonw/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDGaID9Lvxg2RvZIGLAsm2dylyPiA",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】プリンセス・キャリー【湊あくあ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=FdCUeriOuWg",
		Duration:     time.Duration(201000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/FdCUeriOuWg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDdMPrksprBnM6wW5YKNQsrkPwAHw",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】魔法少女まじかる☆ござる【風真いろは/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Xr_VyUqdiNs",
		Duration:     time.Duration(237000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Xr_VyUqdiNs/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBmxKgl1lR4sqcs2Y6IocDrGN3qrw",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】風を仰ぎし麗容な (MVバージョン)【風真いろは/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=XEj6Qxj72fw",
		Duration:     time.Duration(238000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/XEj6Qxj72fw/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCHmUJ10JlfZ8N8AZB0K75BHeTTuA",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】Revival【角巻わため/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=OeruwIW3N1o",
		Duration:     time.Duration(260000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/OeruwIW3N1o/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLD56V2AXAjwyjd8Kv2KBYpkLJz_lw",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】魔眼ウインク (MVバージョン)【鷹嶺ルイ/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=FRQ3NXVJtcg",
		Duration:     time.Duration(218000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/FRQ3NXVJtcg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBDn4QMT7R2-XvWUzLtTAY0CWlWkA",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】TROUBLE “WAN”DER！【戌神ころね/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=byBoyCkHiXE",
		Duration:     time.Duration(222000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/byBoyCkHiXE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDNMiogJFVe4sGj3_fyhqXojJHlSw",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】HIDE & SEEK 〜なかよくケンカしな！〜【ハコス・ベールズ × 兎田ぺこら/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=BeIFfPc216w",
		Duration:     time.Duration(190000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/BeIFfPc216w/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDvy69ru28CdQwqNYQfbRFXTVEbtw",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】I I I【宝鐘マリン&こぼ・かなえる/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=_R-CiL5ODdQ",
		Duration:     time.Duration(205000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/_R-CiL5ODdQ/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAwMLc1bHIMLDq515Jox_gq8_oWYQ",
		UserID:       "1120111914764992512",
	},
	{
		Title:        "【VTuber】Secret Garden【hololive 1st Generation/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=M4hpuRHkNlg",
		Duration:     time.Duration(250000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/M4hpuRHkNlg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDFlQflnfDGq8UkmwoG_42ybt7-LA",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】はじまりの魔法-charm-【魔法少女ホロウィッチ！/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=vIiDgz3FGyU",
		Duration:     time.Duration(222000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/vIiDgz3FGyU/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCPoMBFOze0pyGqFawywqhnz64pSw",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】Sakura Day's【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=fkQ58CDTXyk",
		Duration:     time.Duration(330000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/fkQ58CDTXyk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLADXS5muJqlkatDA0CxcAoqGJCsQA",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】さくら色ハイテンション！【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=lzWzf6WVNlQ",
		Duration:     time.Duration(328000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/lzWzf6WVNlQ/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDjh_ywxdVbFqNmsVQe4NafGGe7Pw",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】至上主義アドトラック【hololive IDOL PROJECT/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=hqTq_KNJJzE",
		Duration:     time.Duration(258000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/hqTq_KNJJzE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBH8es9BFPOmr4mcdxpBLsUH-QrWA",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】Suspect (静止画バージョン)【hololive IDOL PROJECT/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=tjYt4Iiod1Y",
		Duration:     time.Duration(277000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/tjYt4Iiod1Y/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDvJdgEyuHjfiXz0ovThQbRyePYuA",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】NEXT COLOR PLANET【星街すいせい/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=QdM1Wh4ji1k",
		Duration:     time.Duration(272000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/QdM1Wh4ji1k/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBbub8B1mZ9-sY4xcGYCuzRpn0q-Q",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】drop candy (LIVE映像バージョン)【ラプラス・ダークネス/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=YUJqLvCHkyM",
		Duration:     time.Duration(161000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/YUJqLvCHkyM/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBmLYlRekjuPgHUB_LyuBE0cu4AdQ",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】ナイトループ【大神ミオ/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=HOhTJ6OtVZA",
		Duration:     time.Duration(201000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/HOhTJ6OtVZA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCvy-ZYv1tDDdylKGTXAKPeBcf8_A",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】よくばりキュートガール【NEGI☆U/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=NJPGQP5ffGk",
		Duration:     time.Duration(202000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/NJPGQP5ffGk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLARnk6JyapIaM49yyar3iT-RK6g7Q",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】ナンデダメナン？ (静止画バージョン)【NEGI☆U/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=jNstVsN1XC8",
		Duration:     time.Duration(239000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/jNstVsN1XC8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDtk6a-AZrQP2HzywVncFNLdBT1Kw",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】ぺこみこ大戦争！！【兎田ぺこら&さくらみこ/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=EOxY6koay50",
		Duration:     time.Duration(266000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/EOxY6koay50/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCtEZy91EY1IgSx9vizJc2DmI734A",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】ドヤっとVピース☆ (LIVE映像バージョン)【NEGI☆U/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=9Zs4A-x6nr0",
		Duration:     time.Duration(232000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/9Zs4A-x6nr0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDAfsp_EsilScIGOyzfgjuvHohaUg",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】おにおにこんこんわおん【いろはにほへっと あやふぶみ/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=EoXZ71t9ff4",
		Duration:     time.Duration(270000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/EoXZ71t9ff4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDwHCDzTYm1jlVlpPVmwONlj9ZPzg",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】BITE! カム! BITE!【ハコス・ベールズ × 戌神ころね/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=lYTcpxetKXA",
		Duration:     time.Duration(221000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/lYTcpxetKXA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCskpV5G_iUDP6ZpC0IKjEYGk1AIA",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】Tokyo Wabi-Sabi Lullaby【がうる・ぐら/ホロライブEN Myth】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=v8-NJ0qgOM4",
		Duration:     time.Duration(194000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/v8-NJ0qgOM4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLABUTH_njaeNqxoNUEX_9QqN2w_Zw",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】S.T.Y. (静止画バージョン)【常闇トワ/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=RQYMGmFtJNY",
		Duration:     time.Duration(221000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/RQYMGmFtJNY/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCBz3ft_ujUzuqG65rdVZ5r1BJWEA",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】Knock it out!【天音かなた/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Qj7g9rfYvyo",
		Duration:     time.Duration(159000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Qj7g9rfYvyo/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDvst9UefUlFlZKC-hYkSKw0xQHkw",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】Dramatic XViltration - JP ver.【hololive ID 1st Generation/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=vfi6Kqj7kBw",
		Duration:     time.Duration(275000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/vfi6Kqj7kBw/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCkBnluL0qxo0izOYqLvBZjRZnitA",
		UserID:       "970065326605598720",
	},
	{
		Title:        "【VTuber】にゃっはろーわーるど!!!【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=TbQQLQ4UIto",
		Duration:     time.Duration(212000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/TbQQLQ4UIto/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLA9LgbwxiD2SpjSJRSZgzpzjOBetA",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】ブリにゃんモードはピピピのピ♡【ホワイトブリニャン/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=T13cHKx_S-g",
		Duration:     time.Duration(238000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/T13cHKx_S-g/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCFTGKgNR6Cns9IVu2zLyMHPTUkcw",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】save our hearts - Japanese ver.【hololive ID 3rd Generation/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=_55RCIwGDBo",
		Duration:     time.Duration(261000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/_55RCIwGDBo/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLADxmya41FDW2QpmmIA39TbWgsjdA",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】REALITY FANTASY【HOLOLIVE FANTASY/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=5aWdc2rpfyA",
		Duration:     time.Duration(223000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/5aWdc2rpfyA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLD8wH15EtrZC6-D2NCLlBA8DJEIHw",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】ビビデバ【星街すいせい/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕/Romanized】",
		SongURL:      "https://www.youtube.com/watch?v=oYqShwIBYks",
		Duration:     time.Duration(179000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/oYqShwIBYks/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBP_9vtbZ4c31BgcqO5U-cd0Fm3Nw",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】ぐるぐる@まわる@まわルーナ【姫森ルーナ/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=GusyCqVk7Sg",
		Duration:     time.Duration(186000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/GusyCqVk7Sg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBgk3EL1EMllEqbjZB7AyVVFfiCzQ",
		UserID:       "1120111914764992512",
	},
	{
		Title:        "【VTuber】シュガーラッシュ【miComet/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=t8qWzwQwEmk",
		Duration:     time.Duration(193000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/t8qWzwQwEmk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCKn0cQajWjDYTzDvWOkJZSTuWLVg",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】Tear-Gazer (MVバージョン)【博衣こより/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=noGsobFKGR0",
		Duration:     time.Duration(280000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/noGsobFKGR0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDXPvhCkHoogYtK1Z0VljwQevYRTg",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】シンメトリー【ReGLOSS/hololive DEV_IS】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=nPpwQ8SKbMA",
		Duration:     time.Duration(221000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/nPpwQ8SKbMA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCkNGcpGCyLFZuvyLL9tW-DFoHxEQ",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】ピパポ☆ピピプ【BABACORN/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=urNuZvH7bvk",
		Duration:     time.Duration(201000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/urNuZvH7bvk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAqC-YEDC8IuJBo6B01JjRlxx2_WQ",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】ハツコイ♡パティシエール【雪花ラミィ/ホロライブ5期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Na9MkLOtIjk",
		Duration:     time.Duration(199000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Na9MkLOtIjk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAEI02J51Qex4xwwlQePV5VK1yz8w",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】melting (静止画バージョン)【百鬼あやめ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=5DgVEVQ-ias",
		Duration:     time.Duration(231000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/5DgVEVQ-ias/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLChp1KV6-vKXJ3pdnGjTo1Ptr1FiQ",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】New Journey (LIVE映像バージョン)【博衣こより/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=3lrVkUzYTN0",
		Duration:     time.Duration(263000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/3lrVkUzYTN0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDy2GPHwosB2KrUJqf2duCMOePEEg",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】Capture the Moment (静止画バージョン・パート分けなし)【hololive IDOL Project/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=r31Uc1CnUk4",
		Duration:     time.Duration(280000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/r31Uc1CnUk4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCHhtwH_82jrvNzoZU4Ao4cE8R6Jg",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】ユーフォリア【癒月ちょこ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=aSZeHInDzbI",
		Duration:     time.Duration(212000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/aSZeHInDzbI/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDRkUJIK3Nu5BdLiB5YkSU-x_hjzg",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】返信願望【天音かなた/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=m5BsxByzsZE",
		Duration:     time.Duration(263000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/m5BsxByzsZE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDLmvDL3OGSZU3Y_dMg9BoLGCKQ4g",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】きゅんきゅんみこきゅんきゅん♡【さくらみこ/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=eD4LGKAUfVk",
		Duration:     time.Duration(235000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/eD4LGKAUfVk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAJNGHTa20blQT6XKutVOkF-pATjw",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】Stellar Symphony【大空スバル/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=85Vceit9CwA",
		Duration:     time.Duration(190000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/85Vceit9CwA/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCjkM0NE0NY4YwbkVKmLrKFWbhQAw",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】ネコカブリーナ【猫又おかゆ/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=29cEyv08JVw",
		Duration:     time.Duration(204000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/29cEyv08JVw/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDI5tK4sLCw47STU2SycqSWrjeUQw",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】START UP【天音かなた/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=8f8SCf2KVSU",
		Duration:     time.Duration(279000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/8f8SCf2KVSU/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCHzF-lQbb4yRbiy0KwA9bBtvyv8w",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】純粋心【天音かなた/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=j4VhUAthozk",
		Duration:     time.Duration(275000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/j4VhUAthozk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLA3tSfabRhE7jmLFMJvhlV-zqVarQ",
		UserID:       "970065326605598720",
	},
	{
		Title:        "【VTuber】Got Cheat【癒月ちょこ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=_v17agbTx88",
		Duration:     time.Duration(223000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/_v17agbTx88/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCHzVet4fQ99Afy9Hz0JQt6I1fY7w",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】リア充★撲滅運動【紫咲シオン/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=1jpmAHa3JR4",
		Duration:     time.Duration(207000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/1jpmAHa3JR4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAMViC7L7cE1sxS02hANNK0wTgxUg",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】hololive Shuffle Medley 2024【ホロライブ】【オフボーカル/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=2IlQh431YLQ",
		Duration:     time.Duration(739000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/2IlQh431YLQ/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAah4MGou0VNLt1mUVjIVywFaOFzg",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】ブライダルドリーム【兎田ぺこら×宝鐘マリン/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=LvmknJnpdcE",
		Duration:     time.Duration(206000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/LvmknJnpdcE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDmrd7YxYxtyVu4dHjUIfSYmDyQCg",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】Now On Step【角巻わため/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=lt-iqARuPw4",
		Duration:     time.Duration(251000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/lt-iqARuPw4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBzcGBggHn9uIRL7Qfb9UKOBSNx3w",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】かわいこちぇっく!【戌神ころね/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=OH2e1iUWXQ0",
		Duration:     time.Duration(194000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/OH2e1iUWXQ0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBCj3jZwy1zJxEGWW0FVPg1PCa5nA",
		UserID:       "1120111914764992512",
	},
	{
		Title:        "【VTuber】Tear-Gazer (LIVE映像バージョン)【博衣こより/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=XUntv5xEdEY",
		Duration:     time.Duration(282000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/XUntv5xEdEY/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLC9GmmBLovm6-2Db5h4jnXC2u-CVQ",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】まんなかちてん【姫森ルーナ/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Hna5rvNJZe0",
		Duration:     time.Duration(244000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Hna5rvNJZe0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDNjXlQEEEP7h7UlJhPxewqdBGqdQ",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】ω猫【AZKi/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=3Y7XZqnYvWg",
		Duration:     time.Duration(182000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/3Y7XZqnYvWg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLArM04Y4Jveg6-oj23HA3CKdlW_TA",
		UserID:       "173979233725448192",
	},
	{
		Title:        "【VTuber】What an amazing swing【角巻わため/ホロライブ4期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Mu6LDvSNL-E",
		Duration:     time.Duration(194000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Mu6LDvSNL-E/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLD-U5U-1ECgukk_aLyW48VnsZjabA",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】ねねちのギラギラファンミーティング【桃鈴ねね/ホロライブ5期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=ssRylO78-Lc",
		Duration:     time.Duration(257000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/ssRylO78-Lc/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCoClXfaV1WE42TADdcMUoEZwb_jg",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】饗宴ソリダリティ～SHAKE★NABE～【秘密結社holoX/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=zGJAAB9sEM4",
		Duration:     time.Duration(237000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/zGJAAB9sEM4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAu_58VxTNYp7hfCDPZSy7QeKCEoA",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】Merry Holy Date♡【hololive IDOL PROJECT/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=zPNf5W5Vh5U",
		Duration:     time.Duration(353000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/zPNf5W5Vh5U/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDNNjlKeLGn93dDOcj5A_djJVDRbA",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】シンデレラ・マジック (MVバージョン)【紫咲シオン/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=hL_PkPywY-Q",
		Duration:     time.Duration(199000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/hL_PkPywY-Q/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBv7A-bQo5HX1ajB6b5201VOhgL4Q",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】のーばでぃーくりすます！【癒月ちょこ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=yi4J4fzeaK4",
		Duration:     time.Duration(223000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/yi4J4fzeaK4/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAIsnqFR-4v8IsADbDi6DLr-NWgZw",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】エイムに愛されしガール【湊あくあ/ホロライブ2期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=gR_JWR01_PE",
		Duration:     time.Duration(187000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/gR_JWR01_PE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLBfSYzARUACggKKdo8p4jegIzN7JA",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】なかま歌 (MVバージョン)【不知火建設/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=QeiaE_GiEQE",
		Duration:     time.Duration(223000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/QeiaE_GiEQE/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLApBT2S6pK3eP1Znbrkj6JS7Uzaaw",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】風を仰ぎし麗容な (LIVE映像バージョン)【風真いろは/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=7HHSS8H7DhU",
		Duration:     time.Duration(245000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/7HHSS8H7DhU/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDL_74GJjsQKnZ4Mp7yYz42APoo_g",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】右左君君右下上目きゅるんめちょかわ！【沙花叉クロヱ/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=tePfs3rCdSo",
		Duration:     time.Duration(196000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/tePfs3rCdSo/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAZjKbF015ZQHPFwOQFeNLazfUwhg",
		UserID:       "975579735293689856",
	},
	{
		Title:        "【VTuber】アンバランス【博衣こより/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=c6KDkrPoVt0",
		Duration:     time.Duration(222000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/c6KDkrPoVt0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAgFXYZQbDVvONTCtad9GwLO2MNdA",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】ホロホーク【鷹嶺ルイ/ホロライブ6期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=ucselp4nCs8",
		Duration:     time.Duration(217000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/ucselp4nCs8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCWRQIvHNOy2BqDDMZAAwi5OqZ_Zg",
		UserID:       "1120111914764992512",
	},
	{
		Title:        "【VTuber】カメリア【大神ミオ/ホロライブゲーマーズ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=Y5M-ZgjdwGY",
		Duration:     time.Duration(295000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/Y5M-ZgjdwGY/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLA9tg4sQnclnaj0EeFcOSZ7OBFEWg",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】最強女神†ウーサペコラ (静止画バージョン)【兎田ぺこら/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=ewQi-n9Vcyg",
		Duration:     time.Duration(246000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/ewQi-n9Vcyg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCxBvwM4hEVtZgx126Ev2997K7xgA",
		UserID:       "970065326605598720",
	},
	{
		Title:        "【VTuber】Connect:Addict【獅白ぼたん/ホロライブ5期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=4Zl7AB7pD3c",
		Duration:     time.Duration(297000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/4Zl7AB7pD3c/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDf1miaZnA2py191_IUeGQ52YYwBA",
		UserID:       "741144530677399552",
	},
	{
		Title:        "【VTuber】アザミナ (MVバージョン)【ロボ子さん/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=mOdAOBsYav8",
		Duration:     time.Duration(257000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/mOdAOBsYav8/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAQn8xrURL64VCMqpeAxIXJ5ubKCg",
		UserID:       "663442973920329728",
	},
	{
		Title:        "【VTuber】SHINKIRO【GuraMarine/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=-1yEB2osxqQ",
		Duration:     time.Duration(312000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/-1yEB2osxqQ/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLD7FqPbUYsJ9S71T5mHD7TP27MhnQ",
		UserID:       "1190839910949453824",
	},
	{
		Title:        "【VTuber】ザイオン【星街すいせい/ホロライブ0期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=PG3K5pkDOTM",
		Duration:     time.Duration(264000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/PG3K5pkDOTM/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLANL6dkBn7dyHeoGOhkMaNRede89Q",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】光になれ! (静止画バージョン)【ホロライブ運動会実行委員/ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=rQttUtwZpDg",
		Duration:     time.Duration(120000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/rQttUtwZpDg/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCn2XpkVIFQZ7EDA8EoHsppE9NdhQ",
		UserID:       "732665797830246400",
	},
	{
		Title:        "【VTuber】Sing Out (静止画バージョン)【Ayunda Risu/ホロライブID1期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=hHxXCyKcxH0",
		Duration:     time.Duration(244000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/hHxXCyKcxH0/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAEvaBF7CHDpWJf6zXHGTUJMcCu4A",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】ReSound (LIVE映像バージョン)【夜空メル/ホロライブ1期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=e6uXZwzausI",
		Duration:     time.Duration(260000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/e6uXZwzausI/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLC05hTsOEhqIXV0OVmU7B-TYZhNUA",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】BUTA (MVバージョン)【赤井はあと/ホロライブ1期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=GaArGbRLl7I",
		Duration:     time.Duration(209000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/GaArGbRLl7I/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLD27Fp7_L03uQBMdJfOP7jY4KuHug",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】はっぴー (MVバージョン)【白銀ノエル/ホロライブ3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=hRpFWykO0pM",
		Duration:     time.Duration(186000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/hRpFWykO0pM/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLDrYuTlYdOGScGr0xBXRzMbYI66AQ",
		UserID:       "619579362408136704",
	},
	{
		Title:        "【VTuber】恋文前線 (静止画バージョン)【ホロライブ】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=GP1hPVjF28k",
		Duration:     time.Duration(245000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/GP1hPVjF28k/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLAkLFwmdax9Pkg2PK2ob-aw_9Kwag",
		UserID:       "897976914709315584",
	},
	{
		Title:        "【VTuber】Backseat【Kaela Kovalskia/ホロライブID3期生】【インスト版(ガイドメロディ付)/カラオケ字幕】",
		SongURL:      "https://www.youtube.com/watch?v=gkKNMit1gwk",
		Duration:     time.Duration(270000000000),
		ThumbnailURL: "https://i.ytimg.com/vi/gkKNMit1gwk/hqdefault.jpg?sqp=-oaymwEbCKgBEF5IVfKriqkDDggBFQAAiEIYAXABwAEG&rs=AOn4CLCjAj3JTxKe49pE_cg0F2KR2G9npg",
		UserID:       "173979233725448192",
	},
}
