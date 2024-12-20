package main

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync/atomic"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/xoltia/mdk3/queue"
	"github.com/xoltia/mpv"
)

var (
	dequeueEnabled = atomic.Bool{}
)

func showOSD(ctx context.Context, mpvClient *mpv.Client, text string) error {
	_, err := mpvClient.Command(ctx, "show-text", text)
	return err
}

func loopPlayMPV(ctx context.Context, q *queue.Queue, h *queueCommandHandler, mpvClient *mpv.Client, cfg config) {
	if cfg.StartImmediately {
		dequeueEnabled.Store(true)
	}

	// Set osd-duration
	if err := mpvClient.SetProperty(ctx, "osd-duration", 1100); err != nil {
		log.Fatalln("cannot set OSD duration:", err)
	}

	for {
		if ctx.Err() != nil {
			return
		}

		if !dequeueEnabled.Load() {
			showOSD(ctx, mpvClient, "Waiting for /start")
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}

		tx := q.BeginTxn(true)
		song, err := tx.Dequeue()
		if err != nil {
			if err == queue.ErrQueueEmpty {
				tx.Discard()
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
					continue
				}
			}

			log.Println("cannot dequeue:", err)
			tx.Discard()
			break
		}
		next, err := tx.List(0, 10)
		if err != nil {
			log.Println("cannot list:", err)
			tx.Discard()
			break
		}
		if err = tx.Commit(); err != nil {
			log.Println("cannot commit:", err)
			break
		}
		log.Println("playing", song.Title)

		var username string
		userSnowflake, err := discord.ParseSnowflake(song.UserID)
		if err != nil {
			log.Println("cannot parse user id:", err)
		} else {
			member, err := h.s.Member(discord.GuildID(cfg.Discord.Guild), discord.UserID(userSnowflake))
			if err != nil {
				log.Println("cannot get username:", err)
			} else {
				username = member.Nick
				if username == "" {
					username = member.User.DisplayOrUsername()
				}
			}
		}

		var thumbnail image.Image
		actualThumbnail, err := downloadThumbnail(ctx, song.ThumbnailURL)
		if err != nil {
			thumbnail = image.Black
			log.Println("cannot download thumbnail:", err)
		} else {
			thumbnail = actualThumbnail
		}

		hasPoster := false
		previewLocation, err := writePreviewPoster(song, username, next, thumbnail)
		if err != nil {
			log.Println("cannot write preview poster:", err)
		} else {
			if err = mpvClient.LoadFile(ctx, previewLocation, mpv.LoadFileModeReplace); err != nil {
				log.Println("cannot load preview poster:", err)
				continue
			}
			hasPoster = true
		}

		if err = mpvClient.Pause(ctx); err != nil {
			log.Println("cannot set pause:", err)
			continue
		}

		loadingLocation, err := writeLoadingPoster(thumbnail)
		if err != nil {
			log.Println("cannot write loading poster:", err)
		} else {
			if err = mpvClient.LoadFile(ctx, loadingLocation, mpv.LoadFileModeAppend); err != nil {
				log.Println("cannot load loading poster:", err)
				continue
			}
		}

		mode := mpv.LoadFileModeAppend
		if !hasPoster {
			mode = mpv.LoadFileModeReplace
		}
		if err = mpvClient.LoadFile(ctx, song.SongURL, mode); err != nil {
			log.Println("cannot load file:", err)
			continue
		}

		messageContent := ""
		if !cfg.DisablePing {
			messageContent = fmt.Sprintf("<@%s>", song.UserID)
		}
		_, err = h.s.SendMessage(discord.ChannelID(cfg.Discord.Channel), messageContent, discord.Embed{
			Title:       song.Title,
			Description: fmt.Sprintf("Your song is up next! The song will start in %s unless started manually.", cfg.PlaybackTime),
		})

		if err != nil {
			log.Println("cannot send message:", err)
		}

		// Change OSD font size
		restoreFontSize := true
		oldSize, err := mpvClient.GetPropertyFloat(ctx, "osd-font-size")
		if err != nil {
			log.Println("cannot get OSD font size:", err)
			restoreFontSize = false
		} else {
			if err = mpvClient.SetProperty(ctx, "osd-font-size", 30); err != nil {
				log.Println("cannot set OSD font size:", err)
			}
		}

		unpausedCh := make(chan struct{})
		unpauseCheckCtx, cancelUnpauseCheck := context.WithCancel(ctx)
		timeLeft := cfg.PlaybackTime
		go func() {
			for {
				select {
				case <-time.After(time.Second):
					timeLeft -= time.Second
					paused, err := mpvClient.GetPropertyBool(ctx, "pause")
					if err != nil {
						log.Println("cannot get pause state:", err)
						continue
					}
					if !paused {
						close(unpausedCh)
						return
					} else {
						showOSD(ctx, mpvClient, fmt.Sprintf("Starting in %s", timeLeft))
					}
				case <-unpauseCheckCtx.Done():
					return
				}
			}
		}()

		select {
		case <-unpausedCh:
		case <-time.After(cfg.PlaybackTime):
			if err = mpvClient.Play(ctx); err != nil {
				cancelUnpauseCheck()
				log.Println("cannot set pause:", err)
				continue
			}
		}

		cancelUnpauseCheck()

		if restoreFontSize {
			if err = mpvClient.SetProperty(ctx, "osd-font-size", oldSize); err != nil {
				log.Println("cannot restore OSD font size:", err)
			}
		}

		continueCh := make(chan struct{})
		unobserve, err := mpvClient.ObserveProperty(ctx, "idle-active", func(value any) {
			if value.(bool) {
				close(continueCh)
			}
		})
		if err != nil {
			log.Println("cannot observe idle-active:", err)
			continue
		}

		<-continueCh
		if err = unobserve(); err != nil {
			log.Println("cannot unobserve idle-active:", err)
		}
	}
}
