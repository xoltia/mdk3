package main

import (
	"context"
	"fmt"
	"image"
	"log/slog"
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
		slog.DebugContext(ctx, "Start immediately flag set")
		dequeueEnabled.Store(true)
	}

	// Set osd-duration
	if err := mpvClient.SetProperty(ctx, "osd-duration", 1100); err != nil {
		slog.ErrorContext(ctx, "Failed to set OSD duration", slog.String("err", err.Error()))
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

			slog.ErrorContext(ctx, "Error dequeuing", slog.String("err", err.Error()))
			tx.Discard()
			break
		}
		next, err := tx.List(0, 10)
		if err != nil {
			slog.ErrorContext(ctx, "Error listing queue items", slog.String("err", err.Error()))
			tx.Discard()
			break
		}
		if err = tx.Commit(); err != nil {
			slog.ErrorContext(ctx, "Error committing transaction", slog.String("err", err.Error()))
			break
		}
		slog.InfoContext(ctx, "Playing next song", slog.String("member", song.UserID), slog.String("title", song.Title), slog.String("url", song.SongURL))

		var username string
		userSnowflake, err := discord.ParseSnowflake(song.UserID)
		if err != nil {
			slog.ErrorContext(ctx, "Unable to parse user ID", slog.String("err", err.Error()), slog.String("user_id", song.UserID))
		} else {
			member, err := h.s.Member(discord.GuildID(cfg.Discord.Guild), discord.UserID(userSnowflake))
			if err != nil {
				slog.WarnContext(ctx, "Unable to get username", slog.String("err", err.Error()), slog.String("user_id", song.UserID))
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
			slog.WarnContext(ctx, "Unable to download thumbnail", slog.String("err", err.Error()), slog.String("url", song.ThumbnailURL))
		} else {
			thumbnail = actualThumbnail
		}

		hasPoster := false
		previewLocation, err := writePreviewPoster(song, username, next, thumbnail)
		if err != nil {
			slog.ErrorContext(ctx, "Error writing preview poster", slog.String("err", err.Error()))
		} else {
			if err = mpvClient.LoadFile(ctx, previewLocation, mpv.LoadFileModeReplace); err != nil {
				slog.ErrorContext(ctx, "Error loading preview poster file to mpv", slog.String("err", err.Error()))
				continue
			}
			hasPoster = true
		}

		if err = mpvClient.Pause(ctx); err != nil {
			slog.ErrorContext(ctx, "Error pausing mpv", slog.String("err", err.Error()))
			continue
		}

		loadingLocation, err := writeLoadingPoster(thumbnail)
		if err != nil {
			slog.ErrorContext(ctx, "Error writing loading poster", slog.String("err", err.Error()))
		} else {
			if err = mpvClient.LoadFile(ctx, loadingLocation, mpv.LoadFileModeAppend); err != nil {
				slog.ErrorContext(ctx, "Error sending loading poster file to mpv", slog.String("err", err.Error()))
				continue
			}
		}

		mode := mpv.LoadFileModeAppend
		if !hasPoster {
			mode = mpv.LoadFileModeReplace
		}
		if err = mpvClient.LoadFile(ctx, song.SongURL, mode); err != nil {
			slog.ErrorContext(ctx, "Error loading song URL to mpv", slog.String("err", err.Error()))
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
			slog.ErrorContext(ctx, "Unable to send heads up message", slog.String("err", err.Error()))
		}

		// Change OSD font size
		restoreFontSize := true
		oldSize, err := mpvClient.GetPropertyFloat(ctx, "osd-font-size")
		if err != nil {
			slog.WarnContext(ctx, "Unable to get OSD font size", slog.String("err", err.Error()))
			restoreFontSize = false
		} else {
			if err = mpvClient.SetProperty(ctx, "osd-font-size", 30); err != nil {
				slog.WarnContext(ctx, "Unable to set OSD font size", slog.String("err", err.Error()))
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
						slog.ErrorContext(ctx, "Unable to get pause state", slog.String("err", err.Error()))
						continue
					}
					if !paused {
						close(unpausedCh)
						slog.DebugContext(ctx, "Detected false pause state, continuing")
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
		case <-unpauseCheckCtx.Done():
			cancelUnpauseCheck()
			slog.ErrorContext(ctx, "Context cancelled", slog.String("err", unpauseCheckCtx.Err().Error()))
			return
		case <-unpausedCh:
		case <-time.After(cfg.PlaybackTime):
			if err = mpvClient.Play(ctx); err != nil {
				cancelUnpauseCheck()
				slog.ErrorContext(ctx, "Unable to set pause state", slog.String("err", err.Error()))
				continue
			}
		}
		cancelUnpauseCheck()

		if restoreFontSize {
			if err = mpvClient.SetProperty(ctx, "osd-font-size", oldSize); err != nil {
				slog.ErrorContext(ctx, "Error restoring OSD font size", slog.String("err", err.Error()))
			}
		}

		continueCh := make(chan struct{})
		unobserve, err := mpvClient.ObserveProperty(ctx, "idle-active", func(value any) {
			slog.DebugContext(ctx, "Observed change in idle-active state", slog.Bool("idle-active", value.(bool)))
			if value.(bool) {
				close(continueCh)
			}
		})
		if err != nil {
			slog.ErrorContext(ctx, "Unable to observe idle-active property", slog.String("err", err.Error()))
			continue
		}

		<-continueCh
		if err = unobserve(); err != nil {
			slog.ErrorContext(ctx, "Unable to unobserve idle-active property", slog.String("err", err.Error()))
		}
	}
}
