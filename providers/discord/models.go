package discord

import (
	"encoding/json"
	"time"
)

// event is the generic envelope for Discord Gateway communication.
type event struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  int64           `json:"s,omitempty"` // sequence number
	T  string          `json:"t"`           // event name
}

// hello represents the first message received from Discord.
type hello struct {
	HeartbeatInterval int `json:"heartbit_interval"`
}

// identify represents the message sent to tell Discord who's the client.
type identify struct {
	Token      string             `json:"token"`
	Intents    int                `json:"intents"`
	Properties identifyProperties `json:"properties"`
}

type identifyProperties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// resume represents the message sent to tell Discord to resume the connection with the client.
type resume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

// ready represents the message sent by Discord when has accepted your connection and it's ready to stream data.
type ready struct {
	SessionID string `json:"session_id"`
	ResumeURL string `json:"resume_gateway_url"`
}

// message represents a Discord message.
type message struct {
	ID              string       `json:"id"`
	ChannelID       string       `json:"channel_id"`
	Author          user         `json:"author"`
	Content         string       `json:"content"`
	Timestamp       time.Time    `json:"timestamp"`
	Attachments     []attachment `json:"attachments"`
	Embeds          []embed      `json:"embeds"`
	MentionEveryone bool         `json:"mention_everyone"`
	Type            int          `json:"type"`
}

// user represents a Discord user.
type user struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
}

// attachment represents a Discord attachment.
type attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

// embed represents a Discord embed.
type embed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}
