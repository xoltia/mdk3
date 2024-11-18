package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/wader/goutubedl"
	"github.com/xoltia/mdk3/queue"
	"github.com/xoltia/mpv"
)

var (
	queuePath    = flag.String("queue-path", "queue_data", "path to the queue archive")
	playbackTime = flag.Duration("playback-time", 30*time.Second, "time to wait before playing the next song")
	discordToken = flag.String("discord-token", "", "Discord bot token")
	ytdlpPath    = flag.String("ytdlp-path", "", "path to yt-dlp binary")
	userLimit    = flag.Int("user-limit", math.MaxInt, "number of songs a user can queue")
	mpvPath      = flag.String("mpv-path", "mpv", "path to mpv binary")
	guildID      = flag.String("guild-id", "", "guild id to use for commands")
	channelID    = flag.String("channel-id", "", "channel id to use for commands")
	adminRoles   = flag.String("admin-roles", "", "comma separated list of admin role ids")
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()
	goutubedl.Path = *ytdlpPath

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mpvProcess := &mpv.Process{
		Path: *mpvPath,
		Args: []string{"--force-window"},
	}
	defer mpvProcess.Close()

	mpvClient, err := mpvProcess.OpenClient()
	if err != nil {
		log.Fatalln("cannot open mpv client:", err)
	}

	go func() {
		mpvProcess.Wait()
		cancel()
	}()

	log.Println("connected to mpv")

	if *queuePath != "" {
		if err := os.MkdirAll(*queuePath, 0755); err != nil {
			panic(err)
		}
	}

	q, err := queue.OpenQueue(*queuePath)
	if err != nil {
		log.Fatalln("cannot open queue:", err)
	}
	defer q.Close()

	if *discordToken == "" {
		log.Fatalln("no discord token provided")
	}

	log.Println("starting discord bot")

	s := state.New("Bot " + *discordToken)
	roleIDStrings := []string{}
	if *adminRoles != "" {
		roleIDStrings = strings.Split(*adminRoles, ",")
	}
	handler := newHandler(s, q, withUserLimit(*userLimit), withAdminRoles(roleIDStrings))

	s.AddInteractionHandler(handler)
	s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMembers | gateway.IntentGuildMessages)

	guildSnowflake, err := discord.ParseSnowflake(*guildID)
	if err != nil {
		log.Fatalln("cannot parse guild id:", err)
	}

	application, err := s.CurrentApplication()
	if err != nil {
		log.Fatalln("cannot get application:", err)
	}

	s.BulkOverwriteCommands(application.ID, []api.CreateCommandData{})

	if _, err := s.BulkOverwriteGuildCommands(application.ID, discord.GuildID(guildSnowflake), commands); err != nil {
		log.Fatalln("cannot update commands:", err)
	}

	// if err := cmdroute.OverwriteCommands(s, commands); err != nil {
	// 	log.Fatalln("cannot update commands:", err)
	// }

	go loopPlayMPV(ctx, q, handler, mpvClient)

	log.Println("connecting to discord, press Ctrl+C to exit")
	if err := s.Connect(ctx); err != nil {
		log.Println("cannot connect:", err)
	}
}
