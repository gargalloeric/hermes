package telegram

import "context"

// MessageReceiver represents the message provider that is going to provide the updates to the Telegram provider.
type MessageReceiver interface {
	Start(ctx context.Context) error
	Updates() <-chan update
}
