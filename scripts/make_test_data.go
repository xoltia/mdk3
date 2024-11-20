// This script is used to generate test data for the queue package.
// Outputs to stdout a list of struct literals that can be copy-pasted
// into the queue_test.go file.

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/xoltia/mdk3/queue"
)

var (
	discordEpoch = int64(1420070400000)
	currentEpoch = time.Now().UnixMilli()
)

var (
	numberOfTestVideo      = flag.Int("n-songs", 100, "number of test videos")
	numberOfTestSnowflakes = flag.Int("n-snowflakes", 10, "number of test snowflakes")
	snowflakeStartRange    = flag.Int64("timestamp-start", discordEpoch, "start range of snowflake (unix timestamp in milliseconds)")
	snowflakeEndRange      = flag.Int64("timestamp-end", currentEpoch, "end range of snowflake (unix timestamp in milliseconds)")
)

//go:embed test_videos.jsonl
var videosData []byte

var randomSnowflakes []discord.Snowflake

func init() {
	flag.Parse()

	randomSnowflakes = make([]discord.Snowflake, *numberOfTestSnowflakes)
	for i := 0; i < *numberOfTestSnowflakes; i++ {
		timestamp := rand.Int63n(*snowflakeEndRange-*snowflakeStartRange) + *snowflakeStartRange
		randomSnowflakes[i] = discord.NewSnowflake(time.UnixMilli(timestamp))
	}
}

func randomSnowflake() discord.Snowflake {
	return randomSnowflakes[rand.Intn(len(randomSnowflakes))]
}

func printSongAsStructLiteral(song queue.NewSong) {
	template := `{
	Title: %q,
	SongURL: %q,
	Duration: time.Unix(0, %d),
	ThumbnailURL: %q,
	UserID: %q,
},
`

	fmt.Printf(template, song.Title, song.SongURL, song.Duration.Nanoseconds(), song.ThumbnailURL, song.UserID)
}

func main() {
	var v struct {
		Title      string  `json:"title"`
		URL        string  `json:"url"`
		Duration   float64 `json:"duration"`
		Thumbnails []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"thumbnails"`
	}

	decoder := json.NewDecoder(bytes.NewReader(videosData))

	for i := 0; i < *numberOfTestVideo; i++ {
		if err := decoder.Decode(&v); err != nil {
			log.Fatalf("cannot decode video: %v", err)
		}

		song := queue.NewSong{
			Title:        v.Title,
			SongURL:      v.URL,
			Duration:     time.Duration(v.Duration) * time.Second,
			ThumbnailURL: v.Thumbnails[0].URL,
			UserID:       randomSnowflake().String(),
		}

		printSongAsStructLiteral(song)
	}
}
