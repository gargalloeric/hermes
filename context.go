package hermes

import "context"

// Context represents the environment in which a message is handled.
type Context struct {
	ctx      context.Context
	provider Provider // The provider that received the message
	Message  *Message
}

// NewContext creates a fresh context for a message.
func NewContext(ctx context.Context, p Provider, msg *Message) *Context {
	return &Context{
		ctx:      ctx,
		provider: p,
		Message:  msg,
	}
}

// Reply sends a message back to the same chat where the original message originated.
func (c *Context) Reply(text string, opts ...SendOption) error {
	req := MessageRequest{
		RecipientID: c.Message.Sender.ID,
		Text:        text,
		ReplyToID:   c.Message.ID, // Hardcoded behavior: always a reply
	}

	options := &SendOptions{}
	for _, opt := range opts {
		opt(options)
	}

	req.Attachments = options.Attachments

	return c.provider.SendMessage(c.ctx, req)
}

// SendTo allows sending a message to a specific user/group ID on the same platform.
func (c *Context) SendTo(id string, text string, opts ...SendOption) error {
	req := MessageRequest{
		RecipientID: id,
		Text:        text,
	}

	options := &SendOptions{}
	for _, opt := range opts {
		opt(options)
	}

	req.Attachments = options.Attachments

	return c.provider.SendMessage(c.ctx, req)
}
