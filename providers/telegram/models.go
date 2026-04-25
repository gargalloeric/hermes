package telegram

import (
	"encoding/json"
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
	Message  *message `json:"message,omitempty"`
}

// message represents a message sent by a User.
type message struct {
	MessageID int         `json:"message_id"`
	From      *user       `json:"from"`
	Chat      *chat       `json:"chat"`
	Text      string      `json:"text"`
	Caption   string      `json:"caption"`
	Photo     []photoSize `json:"photo"`
	Video     *video      `json:"video"`
	Document  *document   `json:"document"`
	Voice     *voice      `json:"voice"`
	Location  *location   `json:"location"`

	// Events
	NewChatMembers []user `json:"new_chat_members,omitempty"`
	LeftChatMember *user  `json:"left_chat_member,omitempty"`
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

// apiError represents a Telegram provider error.
type apiError struct {
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("telegram api error: %s", e.Message)
}

// payload is the data payload sent to the Telegram API.
type payload struct {
	ChatID           string         `json:"chat_id"`
	Text             string         `json:"text,omitempty"`
	ReplyToMessageID string         `json:"reply_to_message_id,omitempty"`
	Caption          string         `json:"caption,omitempty"`
	Photo            string         `json:"photo,omitempty"`
	Video            string         `json:"video,omitempty"`
	Document         string         `json:"document,omitempty"`
	Media            []payloadMedia `json:"media,omitempty"`
	Action           string         `json:"action,omitempty"`
	MessageID        string         `json:"message_id,omitempty"`
}

// payloadMedia represents a media element of the payload sent to the Telegram API.
type payloadMedia struct {
	Media   string `json:"media"`
	Type    string `json:"type"`
	Caption string `json:"caption,omitempty"`
}

// postResponse represents the aknowledgement response for a sent message.
type postResponse struct {
	Ok          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  *parameters     `json:"parameters,omitempty"`
}
