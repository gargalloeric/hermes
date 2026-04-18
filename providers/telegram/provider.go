package telegram

import (
	"context"
	"time"

	"github.com/gargalloeric/hermes"
)

const (
	apiBase = "https://api.telegram.org"
)

// Telgram serves as the communication bridge between Hermes code and the Telegram provider.
type Telegram struct {
	receiver MessageReceiver
}

func New(token string) *Telegram {
	return &Telegram{
		receiver: newPoller(token),
	}
}

func (t *Telegram) Name() string {
	return "telegram"
}

func (t *Telegram) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	errChan := make(chan error, 1)

	go func() {
		errChan <- t.receiver.Start(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case upd, ok := <-t.receiver.Updates():
			if !ok {
				return nil
			}
			out <- t.mapToHermes(upd)
		}
	}
}

func (t *Telegram) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	return nil, nil
}

func (t *Telegram) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	return nil, nil
}

func (t *Telegram) ActionTimeout() time.Duration {
	return 5 * time.Second
}

func (t *Telegram) SendAction(ctx context.Context, req hermes.ActionRequest) error {
	return nil
}

func (t *Telegram) mapToHermes(upd update) *hermes.Message {
	return nil
}
