package discord

import (
	"context"
	"fmt"
	"net/http"
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

			hMsg := mapMessageToHermes(d.Name(), msg)
			if hMsg != nil && !hMsg.Sender.IsBot {
				out <- hMsg
			}
		}
	}
}

func (d *Discord) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	sendReq := buildPayload(req)

	msg, err := d.sender.executeMessage(ctx, sendReq.endpoint, http.MethodPost, sendReq.payload, sendReq.files)
	if err != nil {
		return nil, err
	}

	return &hermes.SentMessage{
		ID:       msg.ID,
		Platform: d.Name(),
		ChatID:   msg.ChannelID,
	}, nil
}

func (d *Discord) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint := fmt.Sprintf("/channels/%s/messages/%s", target.ChatID, target.ID)

	payload := payload{
		Content: req.Text,
	}

	msg, err := d.sender.executeMessage(ctx, endpoint, http.MethodPatch, payload, nil)
	if err != nil {
		return nil, err
	}

	return &hermes.SentMessage{
		ID:       msg.ID,
		Platform: d.Name(),
		ChatID:   msg.ChannelID,
	}, nil
}

func (d *Discord) ActionTimeout() time.Duration {
	return 10 * time.Second
}

func (d *Discord) SendAction(ctx context.Context, req hermes.ActionRequest) error {
	action := mapAction(req.Action)
	endpoint := fmt.Sprintf("/channels/%s/%s", req.RecipientID, action)

	_, err := d.sender.executeMessage(ctx, endpoint, http.MethodPost, payload{}, nil)

	return err
}
