package hermes

// SendOptions holds configuration for outgoing messages.
type SendOptions struct {
	Attachments []Attachment
}

// SendOption is a function that modifies SendOptions.
type SendOption func(*SendOptions)

// WithImage adds an image attachment to the message.
func WithImage(url string) SendOption {
	return func(so *SendOptions) {
		so.Attachments = append(so.Attachments, Attachment{
			Type: AttachmentImage,
			URL:  url,
		})
	}
}
