package hermes

import "context"

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
}
