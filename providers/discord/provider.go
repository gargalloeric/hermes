package discord

import (
	"context"
	"time"

	"github.com/gargalloeric/hermes"
)

const (
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
	apiURL     = "https://discord.com/api/v10"
)

type Discord struct {
	gateway *gateway
	sender  *sender
}

func New(token string) *Discord {
	return &Discord{
		gateway: newGateway(token, gatewayURL),
		sender:  newSender(token, apiURL),
	}
}

func (d *Discord) Name() string {
	return "discord"
}

func (d *Discord) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	errChan := make(chan error, 1)

	go func() {
		errChan <- d.gateway.Start(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case msg, ok := <-d.gateway.Messages():
			if !ok {
				return nil
			}

			if hMsg := mapMessageToHermes(d.Name(), msg); hMsg != nil {
				out <- hMsg
			}
		}
	}
}

func (d *Discord) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	sendReq := buildPayload(req)

	_, err := d.sender.executeMessage(ctx, sendReq.endpoint, sendReq.payload)
	if err != nil {
		return nil, err
	}

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
