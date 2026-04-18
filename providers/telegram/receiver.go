package telegram

import "context"

// UpdateReceiver represents the message provider that is going to provide the updates to the Telegram provider.
type UpdateReceiver interface {
	Start(ctx context.Context) error
	Updates() <-chan update
}
