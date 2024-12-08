package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/kkdai/youtube/v2"
)

var errAgeRestricted = errors.New("video is age restricted")

type VideoInfo struct {
	URL       string        `json:"url"`
	Platform  string        `json:"platform"`
	ID        string        `json:"video_id"`
	Title     string        `json:"video_title"`
	Duration  time.Duration `json:"video_duration"`
	Thumbnail string        `json:"thumbnail"`
}

var ytClient = youtube.Client{}
var ytdlpPath = "yt-dlp"

func init() {
	youtube.DefaultClient = youtube.WebClient
}

func getGenericVideoInfo(ctx context.Context, videoURL *url.URL) (v *VideoInfo, err error) {
	cmd := exec.CommandContext(ctx, ytdlpPath, "--dump-single-json", videoURL.String())
	out, err := cmd.Output()

	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			if bytes.Contains(exitError.Stderr, []byte("Sign in to confirm your age")) {
				err = errAgeRestricted
			}
		}
		return
	}

	var info struct {
		WebpageURL string `json:"webpage_url"`
		Extractor  string `json:"extractor_key"`
		ID         string `json:"id"`
		Title      string `json:"title"`
		Duration   int    `json:"duration"`
		Thumbnail  string `json:"thumbnail"`
	}

	err = json.Unmarshal(out, &info)
	if err != nil {
		return
	}

	v = &VideoInfo{
		URL:       info.WebpageURL,
		Platform:  info.Extractor,
		ID:        info.ID,
		Title:     info.Title,
		Duration:  time.Duration(info.Duration) * time.Second,
		Thumbnail: info.Thumbnail,
	}
	return
}

func getInfoFromYouTubeBuiltin(ctx context.Context, videoURL *url.URL) (v *VideoInfo, err error) {
	var video *youtube.Video
	if strings.HasPrefix(strings.ToLower(videoURL.Path), "/live/") {
		parts := strings.Split(videoURL.Path, "/")
		video, err = ytClient.GetVideoContext(ctx, parts[len(parts)-1])
	} else {
		video, err = ytClient.GetVideoContext(ctx, videoURL.String())
	}

	if err != nil {
		return
	}

	var thumbnailURL string

	if len(video.Thumbnails) > 0 {
		thumbnailURL = video.Thumbnails[0].URL
	}

	v = &VideoInfo{
		URL:       fmt.Sprintf("https://www.youtube.com/watch?v=%s", video.ID),
		Platform:  "youtube",
		ID:        video.ID,
		Title:     video.Title,
		Duration:  video.Duration,
		Thumbnail: thumbnailURL,
	}

	highestRes := uint(0)
	highestResThumbnail := ""

	for _, thumbnail := range video.Thumbnails {
		res := thumbnail.Width * thumbnail.Height
		if res > highestRes {
			highestRes = res
			highestResThumbnail = thumbnail.URL
		}

		if res == 0 {
			highestResThumbnail = thumbnail.URL
		}
	}

	v.Thumbnail = highestResThumbnail
	return
}

var youtubeHosts = []string{
	"youtu.be",
	"youtube.com",
	"www.youtube.com",
	"m.youtube.com",
}

func isYouTubeLink(videoURL *url.URL) bool {
	return slices.Contains(youtubeHosts, videoURL.Host)
}

func getVideoInfo(ctx context.Context, videoURL *url.URL) (v *VideoInfo, err error) {
	isYouTubeLink := isYouTubeLink(videoURL)

	if isYouTubeLink {
		v, err = getInfoFromYouTubeBuiltin(ctx, videoURL)
		if err != nil {
			log.Println("failed to get video info from youtube builtin:", err)
			v, err = getGenericVideoInfo(ctx, videoURL)
		}
		return
	}

	return getGenericVideoInfo(ctx, videoURL)
}
