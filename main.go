package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/wader/goutubedl"
	"github.com/xoltia/mdk3/queue"
	"github.com/xoltia/mpv"
)

var configFile = flag.String("config", "config.toml", "config file")

func main() {
	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalln("cannot load config:", err)
	}

	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()
	goutubedl.Path = cfg.Binary.YTDLPath

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mpvProcess := mpv.NewProcessWithOptions(mpv.ProcessOptions{
		Path: cfg.Binary.MPVPath,
		Args: []string{"--force-window"},
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
		cancel()
	}()

	log.Println("connected to mpv")

	q, err := queue.OpenQueue(cfg.QueuePath)
	if err != nil {
		log.Println("cannot open queue:", err)
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

	if cfg.Discord.Token == "" {
		log.Println("no discord token provided")
		exitCode = 1
		return
	}

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

	// if err := cmdroute.OverwriteCommands(s, commands); err != nil {
	// 	log.Fatalln("cannot update commands:", err)
	// }

	go loopPlayMPV(ctx, q, handler, mpvClient, cfg)

	log.Println("connecting to discord, press Ctrl+C to exit")
	if err := s.Connect(ctx); err != nil {
		log.Println("cannot connect:", err)
		exitCode = 1
	}
}
