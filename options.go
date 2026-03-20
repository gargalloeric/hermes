package hermes

// SendOptions holds configuration for outgoing messages.
type SendOptions struct {
	Attachments []Attachment
	IsReply     bool
}

// SendOption is a function that modifies SendOptions.
type SendOption func(*SendOptions)

// AsReply marks the message as a reply/quote of the incoming message.
func AsReply() SendOption {
	return func(so *SendOptions) {
		so.IsReply = true
	}
}

// WithImage adds an image attachment to the message.
func WithImage(url string) SendOption {
	return func(so *SendOptions) {
		so.Attachments = append(so.Attachments, Attachment{
			Type: AttachmentImage,
			URL:  url,
		})
	}
}

// ClientOption defines the signature for configuring the Cerberus Client.
type ClientOption func(*Client)

// WithProvider registers a communication platform (like Telegram) to the client.
func WithProvider(p Provider) ClientOption {
	return func(c *Client) {
		c.providers = append(c.providers, p)
	}
}
