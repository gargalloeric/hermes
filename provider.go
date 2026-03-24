package hermes

import (
	"context"
	"time"
)

// MessageRequest is the payload Hermes sends down to the provider to reply.
type MessageRequest struct {
	RecipientID string
	Text        string
	Attachments []Attachment
	ReplyToID   string
}

// Provider defines how Hermes communicates with a chat platform.
type Provider interface {
	// Name returns the platform identifier (e.g., "telegram")
	Name() string
	// Listen connects to the platform and feeds normalized Messages into the channel.
	// It should block until the context is canceled or a fatal error occurs.
	Listen(ctx context.Context, out chan<- *Message) error
	// SendMessage translates the unified request into platform-specific API calls.
	SendMessage(ctx context.Context, req MessageRequest) (*SentMessage, error)
	// EditMessage modifies an existing message on the platform.
	// The target parameter must contain the valid ID and ChatID of the message to be changed.
	EditMessage(ctx context.Context, target *SentMessage, req MessageRequest) (*SentMessage, error)
	// ActionTimeout returns how long an action lasts on this platform.
	// If it returns 0, the action is considered "one-shot" or permanent.
	ActionTimeout() time.Duration
	// SendAction sends a single activity burst.
	SendAction(ctx context.Context, chatID string, action ActionType) error
}
