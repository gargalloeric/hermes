package hermes

import (
	"context"
	"fmt"
)

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

// Send sends a message back to the same chat where the original message originated.
func (c *Context) Send(text string, opts ...SendOption) (*SentMessage, error) {
	req := MessageRequest{
		RecipientID: c.Message.Sender.ID,
		Text:        text,
	}

	options := &SendOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.IsReply {
		req.ReplyToID = c.Message.ID
	}

	req.Attachments = options.Attachments

	return c.provider.SendMessage(c.ctx, req)
}

// SendTo allows sending a message to a specific user/group ID on the same platform.
func (c *Context) SendTo(id string, text string, opts ...SendOption) (*SentMessage, error) {
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

// Edit updates the content of a previously sent message.
// It uses the SentMessage receipt from a previous Send() call as a reference
// to ensure the correct message is targeted on the platform.
func (c *Context) Edit(target *SentMessage, text string) (*SentMessage, error) {
	if target == nil || target.ChatID == "" {
		return nil, fmt.Errorf("cannot edit: target message reference is empty")
	}

	req := MessageRequest{
		RecipientID: target.ChatID,
		Text:        text,
	}

	return c.provider.EditMessage(c.ctx, target, req)
}

// Platform returns the name of the provider that triggered this context.
func (c *Context) Platform() string {
	return c.provider.Name()
}

// Done returns a channel that's closed when the handling context is cancelled.
func (c *Context) Done() <-chan struct{} {
	return c.ctx.Done()
}
