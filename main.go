package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/xoltia/mdk3/queue"
	"github.com/xoltia/mpv"
)

var (
	configFile    = flag.String("config", "config.toml", "config file")
	skipOverwrite = flag.Bool("skip-overwrite", false, "skip overwriting commands")
)

func main() {
	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	cfg, err := loadConfig(*configFile)
	if err != nil {
		switch v := err.(type) {
		case toml.ParseError:
			fmt.Println("An error occurred while parsing the config.")
			fmt.Println(v.ErrorWithUsage())
		case validationErrors:
			fmt.Println("One or more errors occurred while validating the config.")
			fmt.Println("Please fix the following errors and try again:")
			for _, e := range v {
				fmt.Printf(" %s\n", e)
			}
		default:
			fmt.Println("An error occurred while loading the config:", err)
		}
		exitCode = 1
		return
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)
	flag.Parse()
	// goutubedl.Path = cfg.Binary.YTDLPath
	ytdlpPath = cfg.Binary.YTDLPath

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mpvProcess := mpv.NewProcessWithOptions(mpv.ProcessOptions{
		Path:           cfg.Binary.MPVPath,
		Args:           []string{"--force-window"},
		ConnMaxRetries: 10,
		ConnRetryDelay: time.Second * 1,
	})
	defer mpvProcess.Close()

	mpvClient, err := mpvProcess.OpenClient()
	if err != nil {
		slog.ErrorContext(ctx, "Unable to open mpv client", slog.String("err", err.Error()))
		exitCode = 1
		return
	}

	go func() {
		mpvProcess.Wait()
		slog.ErrorContext(ctx, "MPV process exited")
		cancel()
	}()

	slog.InfoContext(ctx, "Connected to MPV")

	q, err := queue.OpenQueue(cfg.QueuePath)
	if err != nil {
		slog.ErrorContext(ctx, "Error opening queue database", slog.String("err", err.Error()))
		if errors.Is(err, queue.ErrVersionMismatch) {
			slog.ErrorContext(ctx, "The current queue data was created with an incompatible version, move/delete it or set a different location in the configuration file")
		}
		exitCode = 1
		return
	}
	defer q.Close()

	go func() {
		err = q.GC()
		if err != nil {
			slog.WarnContext(ctx, "Error calling GC on queue database", slog.String("err", err.Error()))
		}
	}()

	slog.InfoContext(ctx, "Initializing Discord application")

	s := state.New("Bot " + cfg.Discord.Token)
	handler := newHandler(
		s, q,
		withUserLimit(cfg.UserLimit),
		withAdminRoles(cfg.Discord.AdminRoles),
		withPlaybackTime(cfg.PlaybackTime),
	)

	s.AddInteractionHandler(handler)
	s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMembers | gateway.IntentGuildMessages)

	if !*skipOverwrite {
		application, err := s.CurrentApplication()
		if err != nil {
			slog.ErrorContext(ctx, "Unable to get application information", slog.String("err", err.Error()))
			exitCode = 1
			return
		}
		if _, err := s.BulkOverwriteGuildCommands(application.ID, discord.GuildID(cfg.Discord.Guild), commands); err != nil {
			slog.ErrorContext(ctx, "Unable to overwrite application commands", slog.String("err", err.Error()))
			exitCode = 1
			return
		}
	}

	go loopPlayMPV(ctx, q, handler, mpvClient, cfg)

	slog.InfoContext(ctx, "Connecting Discord application")
	if err := s.Connect(ctx); err != nil {
		slog.ErrorContext(ctx, "Error connecting", slog.String("err", err.Error()))
		exitCode = 1
	}
}
