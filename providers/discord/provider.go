package discord

import (
	"context"
	"time"

	"github.com/gargalloeric/hermes"
)

const (
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
)

type Discord struct{}

func (d *Discord) Name() string {
	return "discord"
}

func (d *Discord) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	return nil
}

func (d *Discord) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	return nil, nil
}

func (d *Discord) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	return nil, nil
}

func (d *Discord) ActionTimeout() time.Duration {
	return 10 * time.Second
}

func (d *Discord) SendAction(ctx context.Context, req hermes.ActionRequest) error {
	return nil
}
