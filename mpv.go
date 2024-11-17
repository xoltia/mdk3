package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"time"

	"github.com/blang/mpv"
	"github.com/diamondburned/arikawa/v3/discord"
)

func getSocketPath() string {
	switch runtime.GOOS {
	case "windows":
		return `\\.\pipe\mpvsocket`
	default:
		return "/tmp/mpvsocket"
	}
}

func startMPV(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(
		ctx,
		*mpvPath,
		"--idle",
		fmt.Sprintf("--input-ipc-server=%s", getSocketPath()),
		"--fs",
		"--force-window",
	)

	if err := cmd.Start(); err != nil {
		log.Fatalln("cannot start mpv:", err)
	}

	return cmd
}

func acquireIPC(retries int) *mpv.IPCClient {
	defer func() {
		if r := recover(); r != nil {
			if retries > 3 {
				panic(r)
			}
			log.Println("failed to connect to mpv, retrying")
			acquireIPC(retries + 1)
		}
	}()

	time.Sleep(time.Second * time.Duration(1<<retries))
	return mpv.NewIPCClient(getSocketPath())
}

func showOSD(mpvClient *mpv.Client, text string) error {
	_, err := mpvClient.Exec("show-text", text)
	return err
}

func loopPlayMPV(ctx context.Context, q *queue, h *queueCommandHandler, mpvClient *mpv.Client) {
	guildSnowflake, err := discord.ParseSnowflake(*guildID)
	if err != nil {
		log.Fatalln("cannot parse guild id:", err)
	}

	channelSnowflake, err := discord.ParseSnowflake(*channelID)
	if err != nil {
		log.Fatalln("cannot parse channel id:", err)
	}

	// Set osd-duration
	if err = mpvClient.SetProperty("osd-duration", 1100); err != nil {
		log.Fatalln("cannot set OSD duration:", err)
	}

playLoop:
	for {
		q.mu.Lock()
		song, err := q.dequeue(ctx)
		if err != nil {
			log.Println("cannot dequeue:", err)
			q.mu.Unlock()
			break
		}
		h.decrementUserCount(song.UserID)
		next := q.peek(10)
		q.mu.Unlock()

		log.Println("playing", song.Title)

		var username string
		userSnowflake, err := discord.ParseSnowflake(song.UserID)
		if err != nil {
			log.Println("cannot parse user id:", err)
		} else {
			member, err := h.s.Member(discord.GuildID(guildSnowflake), discord.UserID(userSnowflake))
			if err != nil {
				log.Println("cannot get username:", err)
			} else {
				username = member.User.Username
			}
		}

		hasPoster := false
		previewLocation, err := writePreviewPoster(song, username, next)
		if err != nil {
			log.Println("cannot write preview poster:", err)
		} else {
			if err = mpvClient.Loadfile(previewLocation, mpv.LoadFileModeReplace); err != nil {
				log.Println("cannot load preview poster:", err)
				continue
			}
			hasPoster = true
		}

		if err = mpvClient.SetPause(true); err != nil {
			log.Println("cannot set pause:", err)
			continue
		}

		loadingLocation, err := writeLoadingPoster(song)
		if err != nil {
			log.Println("cannot write loading poster:", err)
		} else {
			if err = mpvClient.Loadfile(loadingLocation, mpv.LoadFileModeAppend); err != nil {
				log.Println("cannot load loading poster:", err)
				continue
			}
		}

		mode := mpv.LoadFileModeAppend
		if !hasPoster {
			mode = mpv.LoadFileModeReplace
		}
		if err = mpvClient.Loadfile(song.SongURL, mode); err != nil {
			log.Println("cannot load file:", err)
			continue
		}

		_, err = h.s.SendMessage(discord.ChannelID(channelSnowflake), fmt.Sprintf("<@%s>", song.UserID), discord.Embed{
			Title:       song.Title,
			Description: "Your song is up next! The song will start in 30 seconds unless started manually.",
		})

		if err != nil {
			log.Println("cannot send message:", err)
		}

		// Change OSD font size
		restoreFontSize := true
		oldSize, err := mpvClient.GetFloatProperty("osd-font-size")
		if err != nil {
			log.Println("cannot get OSD font size:", err)
			restoreFontSize = false
		} else {
			if err = mpvClient.SetProperty("osd-font-size", 30); err != nil {
				log.Println("cannot set OSD font size:", err)
			}
		}

		unpausedCh := make(chan struct{})
		unpauseCheckCtx, cancelUnpauseCheck := context.WithCancel(ctx)
		timeLeft := *playbackTime
		go func() {
			for {
				select {
				case <-time.After(time.Second):
					timeLeft -= time.Second
					paused, err := mpvClient.GetBoolProperty("pause")
					if err != nil {
						log.Println("cannot get pause state:", err)
						continue
					}
					if !paused {
						close(unpausedCh)
						return
					} else {
						showOSD(mpvClient, fmt.Sprintf("Starting in %s", timeLeft))
					}
				case <-unpauseCheckCtx.Done():
					return
				}
			}
		}()

		select {
		case <-unpausedCh:
		case <-time.After(*playbackTime):
			if err = mpvClient.SetPause(false); err != nil {
				cancelUnpauseCheck()
				log.Println("cannot set pause:", err)
				continue
			}
		}

		cancelUnpauseCheck()

		if restoreFontSize {
			if err = mpvClient.SetProperty("osd-font-size", oldSize); err != nil {
				log.Println("cannot restore OSD font size:", err)
			}
		}

		for {
			idle, err := mpvClient.GetBoolProperty("idle-active")
			if err != nil {
				log.Println("cannot get idle state:", err)
				continue playLoop
			}
			if idle {
				break
			}
			time.Sleep(time.Second)
		}
	}
}
