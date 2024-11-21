package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/api/cmdroute"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"github.com/xoltia/mdk3/queue"
)

var commands = []api.CreateCommandData{
	{
		Name:        "enqueue",
		Description: "Add a song to the queue.",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "url",
				Description: "The URL of the song to add.",
				Required:    true,
			},
		},
	},
	{
		Name:        "list",
		Description: "List the songs in the queue.",
	},
	{
		Name:        "remove",
		Description: "Remove a song from the queue.",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "id",
				Description: "The ID of the song to remove.",
				Required:    true,
			},
		},
	},
	{
		Name:        "swap",
		Description: "Swap a queued song with another.",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "id",
				Description: "The ID of the song to swap.",
				Required:    true,
			},
			&discord.StringOption{
				OptionName:  "url",
				Description: "The URL of the song to swap with.",
				Required:    true,
			},
		},
	},
	{
		Name:        "move",
		Description: "Move a song to a different position in the queue.",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "id",
				Description: "The ID of the song to move.",
				Required:    true,
			},
			&discord.IntegerOption{
				OptionName:  "position",
				Description: "The position to move the song to.",
				Required:    true,
			},
		},
	},
	{
		Name:        "start",
		Description: "Start playing the queue.",
	},
	{
		Name:        "stop",
		Description: "Stop playing the queue. Will not stop the current song.",
	},
}

type queueCommandHandler struct {
	*cmdroute.Router
	s            *state.State
	q            *queue.Queue
	pageSize     int
	userLimit    int
	userCountsMu sync.Mutex
	userCounts   map[string]int
	adminRoles   []discord.RoleID
}

type queueCommandHandlerOption func(*queueCommandHandler)

func withUserLimit(limit int) queueCommandHandlerOption {
	return func(h *queueCommandHandler) {
		h.userLimit = limit
	}
}

func withAdminRoles(roles []string) queueCommandHandlerOption {
	return func(h *queueCommandHandler) {
		for _, role := range roles {
			roleID, err := discord.ParseSnowflake(role)
			if err != nil {
				panic(err)
			}
			h.adminRoles = append(h.adminRoles, discord.RoleID(roleID))
		}
	}
}

// func withPageSize(size int) queueCommandHandlerOption {
// 	return func(h *queueCommandHandler) {
// 		h.pageSize = size
// 	}
// }

func newHandler(s *state.State, q *queue.Queue, options ...queueCommandHandlerOption) *queueCommandHandler {
	h := &queueCommandHandler{
		s:          s,
		q:          q,
		userLimit:  1,
		pageSize:   5,
		userCounts: make(map[string]int),
		adminRoles: []discord.RoleID{},
	}

	for _, opt := range options {
		opt(h)
	}

	q.Iterate(func(song queue.QueuedSong) bool {
		h.userCounts[song.UserID]++
		return true
	})

	h.s.AddInteractionHandlerFunc(h.handleComponentInteraction)
	h.Router = cmdroute.NewRouter()
	h.Use(cmdroute.Deferrable(s, cmdroute.DeferOpts{}))
	h.AddFunc("enqueue", h.cmdEnqueue)
	h.AddFunc("list", h.cmdList)
	h.AddFunc("remove", h.cmdRemove)
	h.AddFunc("swap", h.cmdSwap)
	h.AddFunc("move", h.cmdMove)
	h.AddFunc("start", h.cmdStart)
	h.AddFunc("stop", h.cmdStop)

	return h
}

// decrementUserCount decrements the user count for the given user ID
// (such as when a song is dequeued). Must have the queue lock before calling.
func (h *queueCommandHandler) decrementUserCount(userID string) {
	h.userCountsMu.Lock()
	defer h.userCountsMu.Unlock()
	if count := h.userCounts[userID]; count > 0 {
		h.userCounts[userID]--
	}
}

func (h *queueCommandHandler) cmdStart(_ context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	if !h.isAdmin(data.Event.Member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to start the queue."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	message := "Queue playback started."
	if dequeueEnabled.Swap(true) {
		message = "Queue playback already started."
	}
	return &api.InteractionResponseData{
		Content:         option.NewNullableString(message),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdStop(_ context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	if !h.isAdmin(data.Event.Member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to stop the queue."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	message := "Queue playback stopped."
	if !dequeueEnabled.Swap(false) {
		message = "Queue playback already stopped."
	}
	return &api.InteractionResponseData{
		Content:         option.NewNullableString(message),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdEnqueue(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	var options struct {
		URL string `discord:"url"`
	}

	if err := data.Options.Unmarshal(&options); err != nil {
		return errorResponse(err)
	}

	u, err := url.Parse(options.URL)
	if err != nil {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Invalid URL."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	video, err := getVideoInfo(ctx, u)
	if err != nil {
		return errorResponse(err)
	}

	s := queue.NewSong{
		UserID:       data.Event.Member.User.ID.String(),
		Title:        video.Title,
		SongURL:      video.URL,
		ThumbnailURL: video.Thumbnail,
		Duration:     video.Duration,
	}

	tx := h.q.BeginTxn(true)
	defer tx.Discard()

	h.userCountsMu.Lock()
	defer h.userCountsMu.Unlock()
	userCount := h.userCounts[s.UserID]

	adminPass := false
	if userCount >= h.userLimit && !h.isAdmin(data.Event.Member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You have reached the limit of songs you can enqueue."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	} else if userCount >= h.userLimit {
		adminPass = true
	}

	queueDuration := time.Duration(0)

	lastSong, err := tx.LastDequeued()
	if err != nil && !errors.Is(err, queue.ErrSongNotFound) {
		log.Printf("unable to get last dequeue: %s", err)
	} else if err == nil {
		queueDuration += lastSong.Duration - time.Since(lastSong.DequeuedAt)
	}

	err = tx.IterateFromHead(func(song queue.QueuedSong) bool {
		queueDuration += song.Duration
		return true
	})
	if err != nil {
		return errorResponse(err)
	}

	queuedID, err := tx.Enqueue(s)
	if err != nil {
		return errorResponse(err)
	}
	h.userCounts[s.UserID]++

	queued, err := tx.GetByID(queuedID)
	if err != nil {
		log.Println("cannot find queued song:", err)
		return errorResponse(err)
	}

	queuePosition, err := tx.Count()
	if err != nil {
		log.Println("cannot get queue position:", err)
		return errorResponse(err)
	}

	playTimeString := "Next"
	if queuePosition > 1 {
		queueDuration += (*playbackTime) * time.Duration(queuePosition-1)
		playTime := time.Now().Add(queueDuration)
		playTimeString = fmt.Sprintf("<t:%d:t>", playTime.Unix())
	}

	if err = tx.Commit(); err != nil {
		log.Println("cannot commit transaction:", err)
		return errorResponse(err)
	}

	embed := discord.NewEmbed()
	embed.Title = "Song Enqueued"
	if adminPass {
		embed.Title += " (Limit Bypassed)"
	}
	embed.Description = s.Title
	embed.Thumbnail = &discord.EmbedThumbnail{URL: s.ThumbnailURL}
	embed.Footer = &discord.EmbedFooter{Text: fmt.Sprintf("ID: %s", queued.Slug)}
	embed.Fields = append(embed.Fields, discord.EmbedField{
		Name:   "Queue Position",
		Value:  fmt.Sprintf("%d", queuePosition),
		Inline: true,
	})
	embed.Fields = append(embed.Fields, discord.EmbedField{
		Name:   "Earliest Play Time",
		Value:  playTimeString,
		Inline: true,
	})

	return &api.InteractionResponseData{
		Embeds:          &[]discord.Embed{*embed},
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdList(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	embed := discord.NewEmbed()
	embed.Title = "Current Queue"

	tx := h.q.BeginTxn(false)
	defer tx.Discard()

	empty, err := tx.Empty()
	if err != nil {
		log.Println("cannot check if queue is empty:", err)
		return errorResponse(err)
	}

	if empty {
		embed.Description = "The queue is empty."
		return &api.InteractionResponseData{
			Embeds:          &[]discord.Embed{*embed},
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	songs, err := tx.List(0, h.pageSize)
	if err != nil {
		log.Println("cannot list songs:", err)
		return errorResponse(err)
	}

	for i, song := range songs {
		embed.Fields = append(embed.Fields, discord.EmbedField{
			Name:  fmt.Sprintf("%d. %s", i+1, song.Title),
			Value: fmt.Sprintf("ID: %s | Queued by <@%s>", song.Slug, song.UserID),
		})
	}

	buttons := []discord.InteractiveComponent{
		&discord.ButtonComponent{
			Label:    "Refresh",
			Style:    discord.SecondaryButtonStyle(),
			CustomID: discord.ComponentID(fmt.Sprintf("list_page:0:%d", time.Now().UnixMilli())),
		},
	}

	count, err := tx.Count()
	if err != nil {
		log.Println("cannot get queue count:", err)
		return errorResponse(err)
	}

	if count > h.pageSize {
		buttons = append(buttons, &discord.ButtonComponent{
			Label:    "Next",
			Style:    discord.PrimaryButtonStyle(),
			CustomID: discord.ComponentID(fmt.Sprintf("list_page:1:%d", time.Now().UnixMilli())),
		})
	}

	return &api.InteractionResponseData{
		Embeds:          &[]discord.Embed{*embed},
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
		Components: &discord.ContainerComponents{
			(*discord.ActionRowComponent)(&buttons),
		},
	}
}

func (h *queueCommandHandler) cmdRemove(_ context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	var options struct {
		ID string `discord:"id"`
	}

	if err := data.Options.Unmarshal(&options); err != nil {
		return errorResponse(err)
	}

	tx := h.q.BeginTxn(true)
	defer tx.Discard()

	slug := options.ID
	song, err := tx.GetBySlug(slug)
	if err != nil && err != queue.ErrSongNotFound {
		log.Println("cannot find song by slug:", err)
		return errorResponse(err)
	}

	if err == queue.ErrSongNotFound {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	member := data.Event.Member
	if member.User.ID.String() != song.UserID && !h.isAdmin(member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to remove this song."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	if err := tx.Remove(song.ID); err != nil {
		log.Println("cannot remove song by slug:", err)
		return errorResponse(err)
	}

	if err := tx.Commit(); err != nil {
		log.Println("cannot commit transaction:", err)
		return errorResponse(err)
	}

	h.decrementUserCount(song.UserID)

	return &api.InteractionResponseData{
		Content:         option.NewNullableString("Song removed."),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdSwap(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	var options struct {
		ID  string `discord:"id"`
		URL string `discord:"url"`
	}

	if err := data.Options.Unmarshal(&options); err != nil {
		return errorResponse(err)
	}

	u, err := url.Parse(options.URL)
	if err != nil {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Invalid URL."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	video, err := getVideoInfo(ctx, u)
	if err != nil {
		return errorResponse(err)
	}

	tx := h.q.BeginTxn(true)
	defer tx.Discard()

	slug := options.ID
	song, err := tx.GetBySlug(slug)
	if err != nil && err != queue.ErrSongNotFound {
		log.Println("cannot find song by slug:", err)
		return errorResponse(err)
	}

	if err == queue.ErrSongNotFound {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	member := data.Event.Member
	if member.User.ID.String() != song.UserID && !h.isAdmin(member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to swap this song."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	err = tx.Update(song.ID, queue.NewSong{
		UserID:       song.UserID,
		Title:        video.Title,
		SongURL:      video.URL,
		ThumbnailURL: video.Thumbnail,
		Duration:     video.Duration,
	})
	if err != nil {
		log.Println("cannot update song by slug:", err)
		return errorResponse(err)
	}

	if err := tx.Commit(); err != nil {
		log.Println("cannot commit transaction:", err)
		return errorResponse(err)
	}

	embed := discord.NewEmbed()
	embed.Title = "Song Swapped"
	embed.Description = video.Title
	embed.Thumbnail = &discord.EmbedThumbnail{URL: video.Thumbnail}
	embed.Footer = &discord.EmbedFooter{Text: fmt.Sprintf("ID: %s", slug)}

	return &api.InteractionResponseData{
		Embeds:          &[]discord.Embed{*embed},
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdMove(_ context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	var options struct {
		ID       string `discord:"id"`
		Position int    `discord:"position"`
	}

	if err := data.Options.Unmarshal(&options); err != nil {
		return errorResponse(err)
	}

	if !h.isAdmin(data.Event.Member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to move songs."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	tx := h.q.BeginTxn(true)
	defer tx.Discard()

	slug := options.ID
	song, err := tx.GetBySlug(slug)
	if err != nil && err != queue.ErrSongNotFound {
		log.Println("cannot find song by slug:", err)
		return errorResponse(err)
	}

	if err == queue.ErrSongNotFound {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	count, err := tx.Count()
	if err != nil {
		log.Println("cannot get queue count:", err)
		return errorResponse(err)
	}

	if options.Position < 1 || options.Position > count {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Invalid position."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	err = tx.Move(song.ID, options.Position-1)
	if err != nil {
		log.Println("cannot move song:", err)
		return errorResponse(err)
	}

	if err := tx.Commit(); err != nil {
		log.Println("cannot commit transaction:", err)
		return errorResponse(err)
	}

	return &api.InteractionResponseData{
		Content:         option.NewNullableString("Song moved."),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) isAdmin(member *discord.Member) bool {
	return slices.ContainsFunc(h.adminRoles, func(role discord.RoleID) bool {
		return slices.Contains(member.RoleIDs, discord.RoleID(role))
	})
}

func errorResponse(err error) *api.InteractionResponseData {
	return &api.InteractionResponseData{
		Content:         option.NewNullableString("**Error:** " + err.Error()),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) handleComponentInteraction(ev *discord.InteractionEvent) *api.InteractionResponse {
	if ev.Data.InteractionType() != discord.ComponentInteractionType {
		return nil
	}

	data := ev.Data.(discord.ComponentInteraction)
	componentID := string(data.ID())
	parts := strings.SplitN(componentID, ":", 3)
	if len(parts) != 3 {
		return nil
	}

	switch parts[0] {
	case "list_page":
		pageNumber, _ := strconv.Atoi(parts[1])
		return h.handleListPage(pageNumber)
	default:
		return nil
	}
}

func (h *queueCommandHandler) handleListPage(pageNumber int) *api.InteractionResponse {
	embed := discord.NewEmbed()
	embed.Title = "Current Queue"

	tx := h.q.BeginTxn(false)
	defer tx.Discard()

	empty, err := tx.Empty()
	if err != nil {
		log.Println("cannot check if queue is empty:", err)
		return &api.InteractionResponse{
			Type: api.UpdateMessage,
			Data: errorResponse(err),
		}
	}

	if empty {
		embed.Description = "The queue is empty."
		return &api.InteractionResponse{
			Type: api.UpdateMessage,
			Data: &api.InteractionResponseData{
				Embeds: &[]discord.Embed{*embed},
				Components: &discord.ContainerComponents{
					&discord.ActionRowComponent{
						&discord.ButtonComponent{
							Label:    "Refresh",
							Style:    discord.SecondaryButtonStyle(),
							CustomID: discord.ComponentID(fmt.Sprintf("list_page:0:%d", time.Now().UnixMilli())),
						},
					},
				},
				AllowedMentions: &api.AllowedMentions{},
			},
		}
	}

	count, err := tx.Count()
	if err != nil {
		log.Println("cannot get queue count:", err)
		return &api.InteractionResponse{
			Type: api.UpdateMessage,
			Data: errorResponse(err),
		}
	}

	if pageNumber < 0 {
		pageNumber = 0
	}

	start := pageNumber * h.pageSize
	end := start + h.pageSize

	if start >= count {
		start = max(0, count-h.pageSize)
	}

	if end > count {
		end = count
	}

	songs, err := tx.List(start, h.pageSize)
	if err != nil {
		log.Println("cannot list songs:", err)
		return &api.InteractionResponse{
			Type: api.UpdateMessage,
			Data: errorResponse(err),
		}
	}

	for i, song := range songs {
		embed.Fields = append(embed.Fields, discord.EmbedField{
			Name:  fmt.Sprintf("%d. %s", start+i+1, song.Title),
			Value: fmt.Sprintf("ID: %s | Queued by <@%s>", song.Slug, song.UserID),
		})
	}

	buttons := make([]discord.InteractiveComponent, 0, 3)

	buttons = append(buttons, &discord.ButtonComponent{
		Label:    "Previous",
		Style:    discord.PrimaryButtonStyle(),
		CustomID: discord.ComponentID(fmt.Sprintf("list_page:%d:%d", pageNumber-1, time.Now().UnixMilli())),
		Disabled: pageNumber == 0,
	})

	buttons = append(buttons, &discord.ButtonComponent{
		Label:    "Refresh",
		Style:    discord.SecondaryButtonStyle(),
		CustomID: discord.ComponentID(fmt.Sprintf("list_page:%d:%d", pageNumber, time.Now().UnixMilli())),
	})

	buttons = append(buttons, &discord.ButtonComponent{
		Label:    "Next",
		Style:    discord.PrimaryButtonStyle(),
		CustomID: discord.ComponentID(fmt.Sprintf("list_page:%d:%d", pageNumber+1, time.Now().UnixMilli())),
		Disabled: end == count,
	})

	return &api.InteractionResponse{
		Type: api.UpdateMessage,
		Data: &api.InteractionResponseData{
			Embeds: &[]discord.Embed{*embed},
			Components: &discord.ContainerComponents{
				(*discord.ActionRowComponent)(&buttons),
			},
			AllowedMentions: &api.AllowedMentions{},
		},
	}
}
