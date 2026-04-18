package telegram

import (
	"fmt"
	"time"
)

// response represents a response from the Telegram API.
type response struct {
	Ok          bool        `json:"ok"`
	Result      []update    `json:"result"`
	Description string      `json:"description,omitempty"`
	Parameters  *parameters `json:"parameters,omitempty"`
}

// parameters describe why a request was unsuccessful.
type parameters struct {
	// Number of seconds left to wait before the request can be repeated.
	RetryAfter int `json:"retry_after,omitempty"`
}

// update represents an update from the Telegram API.
type update struct {
	UpdateID int      `json:"update_id"`
	Message  *message `json:"message"`
}

// message represents a message sent by a User.
type message struct {
	MessageID int         `json:"message_id"`
	From      *user       `json:"user"`
	Chat      *chat       `json:"chat"`
	Text      string      `json:"text"`
	Caption   string      `json:"caption"`
	Photo     []photoSize `json:"photo"`
	Video     *video      `json:"video"`
	Document  *document   `json:"document"`
	Voice     *voice      `json:"voice"`
	Location  *location   `json:"location"`
}

// user represents a Telegram user.
type user struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

// chat represents a Telegram chat.
type chat struct {
	ID int64 `json:"id"`

	// Type of the chat, can be either “private”, “group”, “supergroup” or “channel”
	Type string `json:"type"`

	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

// photoSize represents one size of a photo or a file / sticker thumbnail.
type photoSize struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"file_size"`
}

// video represents a video file.
type video struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Duration int    `json:"duration"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int    `json:"file_size"`
}

// document represents a general file.
type document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int    `json:"file_size"`
}

// voice represents a voice note.
type voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type"`
	FileSize int    `json:"file_size"`
}

// location represents a point on the map.
type location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type tgError struct {
	Message    string
	RetryAfter time.Duration
}

func (e *tgError) Error() string {
	return fmt.Sprintf("telegram api error: %s", e.Message)
}
