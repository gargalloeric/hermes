package discord

import (
	"encoding/json"
	"fmt"
	"time"
)

// dsPayload is the generic envelope for Discord Gateway communication
type dsPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  int64           `json:"s,omitempty"` // Sequence number
	T  string          `json:"t,omitempty"` // Event name
}

// dsHello is the first message received from Discord
type dsHello struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// dsIdentify is the payload we send to authenticate
type dsIdentity struct {
	Token      string               `json:"token"`
	Intents    int                  `json:"intents"`
	Properties dsIdentifyProperties `json:"properties"`
}

type dsIdentifyProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// dsMessage represents a Discord message object
type dsMessage struct {
	ID              string         `json:"id"`
	ChannelID       string         `json:"channel_id"`
	Author          dsUser         `json:"author"`
	Content         string         `json:"content"`
	Timestamp       time.Time      `json:"timestamp"`
	Attachments     []dsAttachment `json:"attachments"`
	Embeds          []dsEmbed      `json:"embeds"`
	MentionEveryone bool           `json:"mention_everyone"`
	Type            int            `json:"type"`
}

type dsUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
}

type dsAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type dsEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// dsResponse acts as the internal envelope, mirroring the Telegram pattern.
type dsResponse struct {
	dsMessage
	Ok          bool    `json:"-"`
	Description string  `json:"message,omitempty"`     // Populated on Error
	ErrorCode   int     `json:"code,omitempty"`        // Populated on Error
	RetryAfter  float64 `json:"retry_after,omitempty"` // Populated on 429
}

type dsError struct {
	Code       int
	Message    string
	RetryAfter time.Duration
}

func (e *dsError) Error() string {
	return fmt.Sprintf("discord API error (%d): %s", e.Code, e.Message)
}

type dsFile struct {
	FileName    string
	Data        []byte
	ContentType string
}
