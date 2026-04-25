package telegram

import "context"

// reveiver represents the message receiver that is going to provide the updates to the Telegram provider.
type reveiver interface {
	Start(ctx context.Context) error
	Updates() <-chan update
}
