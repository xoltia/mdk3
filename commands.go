package main

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/api/cmdroute"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
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
}

type queueCommandHandler struct {
	*cmdroute.Router
	s         *state.State
	q         *queue
	pageSize  int
	userLimit int
	// gaurded by queue mutex
	userCounts map[string]int
	adminRoles []discord.RoleID
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

func newHandler(s *state.State, q *queue, options ...queueCommandHandlerOption) *queueCommandHandler {
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

	for _, song := range q.songs {
		h.userCounts[song.UserID]++
	}

	h.s.AddInteractionHandlerFunc(h.handleComponentInteraction)
	h.Router = cmdroute.NewRouter()
	h.Use(cmdroute.Deferrable(s, cmdroute.DeferOpts{}))
	h.AddFunc("enqueue", h.cmdEnqueue)
	h.AddFunc("list", h.cmdList)
	h.AddFunc("remove", h.cmdRemove)
	h.AddFunc("swap", h.cmdSwap)
	h.AddFunc("move", h.cmdMove)

	return h
}

// decrementUserCount decrements the user count for the given user ID
// (such as when a song is dequeued). Must have the queue lock before calling.
func (h *queueCommandHandler) decrementUserCount(userID string) {
	if count := h.userCounts[userID]; count > 0 {
		h.userCounts[userID]--
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

	s := song{
		UserID:       data.Event.Member.User.ID.String(),
		Title:        video.Title,
		SongURL:      video.URL,
		ThumbnailURL: video.Thumbnail,
		Duration:     duration(video.Duration),
	}

	h.q.mu.Lock()
	defer h.q.mu.Unlock()

	userCount := h.userCounts[s.UserID]

	if userCount >= h.userLimit {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You have reached the limit of songs you can enqueue."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	queued := h.q.enqueue(s)

	embed := discord.NewEmbed()
	embed.Title = "Song Enqueued"
	embed.Description = s.Title
	embed.Thumbnail = &discord.EmbedThumbnail{URL: s.ThumbnailURL}
	embed.Footer = &discord.EmbedFooter{Text: fmt.Sprintf("ID: %s", queued.Slug)}

	return &api.InteractionResponseData{
		Embeds:          &[]discord.Embed{*embed},
		AllowedMentions: &api.AllowedMentions{},
	}
}

func (h *queueCommandHandler) cmdList(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	embed := discord.NewEmbed()
	embed.Title = "Current Queue"

	h.q.mu.RLock()
	defer h.q.mu.RUnlock()

	if h.q.len() == 0 {
		embed.Description = "The queue is empty."
		return &api.InteractionResponseData{
			Embeds:          &[]discord.Embed{*embed},
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	for i, song := range h.q.peek(h.pageSize) {
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

	if h.q.len() > h.pageSize {
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

	h.q.mu.Lock()
	defer h.q.mu.Unlock()

	slug := options.ID
	idx := h.q.findIndexBySlug(slug)
	if idx == -1 {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	song := h.q.songs[idx]
	member := data.Event.Member
	if member.User.ID.String() != song.UserID && !h.isAdmin(member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to remove this song."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	h.q.removeIndex(idx)

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

	h.q.mu.Lock()
	defer h.q.mu.Unlock()

	slug := options.ID
	idx := h.q.findIndexBySlug(slug)
	if idx == -1 {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	member := data.Event.Member
	if member.User.ID.String() != h.q.songs[idx].UserID && !h.isAdmin(member) {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("You are not allowed to swap this song."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	h.q.update(idx, song{
		Title:        video.Title,
		SongURL:      video.URL,
		ThumbnailURL: video.Thumbnail,
		Duration:     duration(video.Duration),
	})

	// TODO: show earliest play time
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

	h.q.mu.Lock()
	defer h.q.mu.Unlock()

	slug := options.ID
	idx := h.q.findIndexBySlug(slug)
	if idx == -1 {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Song not found."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	if options.Position < 1 || options.Position > h.q.len() {
		return &api.InteractionResponseData{
			Content:         option.NewNullableString("Invalid position."),
			Flags:           discord.EphemeralMessage,
			AllowedMentions: &api.AllowedMentions{},
		}
	}

	h.q.move(idx, options.Position-1)

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

	h.q.mu.RLock()
	defer h.q.mu.RUnlock()

	if h.q.len() == 0 {
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

	if pageNumber < 0 {
		pageNumber = 0
	}

	start := pageNumber * h.pageSize
	end := start + h.pageSize

	if start >= len(h.q.songs) {
		start = max(0, len(h.q.songs)-h.pageSize)
	}

	if end > len(h.q.songs) {
		end = len(h.q.songs)
	}

	for i, song := range h.q.songs[start:end] {
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
		Disabled: end == len(h.q.songs),
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
