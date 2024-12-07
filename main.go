package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/wader/goutubedl"
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

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()
	goutubedl.Path = cfg.Binary.YTDLPath

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
		log.Println("cannot open mpv client:", err)
		exitCode = 1
		return
	}

	go func() {
		mpvProcess.Wait()
		log.Println("mpv process exited")
		cancel()
	}()

	log.Println("connected to mpv")

	q, err := queue.OpenQueue(cfg.QueuePath)
	if err != nil {
		log.Println("cannot open queue:", err)
		if errors.Is(err, queue.ErrVersionMismatch) {
			log.Println("the queue version is incompatible, please change the queue path or delete the existing queue")
		}
		exitCode = 1
		return
	}
	defer q.Close()

	go func() {
		err = q.GC()
		if err != nil {
			log.Println("cannot gc queue:", err)
		}
	}()

	log.Println("starting discord bot")

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
			log.Println("cannot get application:", err)
			exitCode = 1
			return
		}
		if _, err := s.BulkOverwriteGuildCommands(application.ID, discord.GuildID(cfg.Discord.Guild), commands); err != nil {
			log.Println("cannot update commands:", err)
			exitCode = 1
			return
		}
	}

	go loopPlayMPV(ctx, q, handler, mpvClient, cfg)

	log.Println("connecting to discord, press Ctrl+C to exit")
	if err := s.Connect(ctx); err != nil {
		log.Println("cannot connect:", err)
		exitCode = 1
	}
}
