package discord

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gargalloeric/hermes"
)

type sender struct {
	token      string
	baseURL    string
	client     *http.Client
	maxRetries int
}

type sendRequest struct {
	endpoint string
	payload  payload
}

func newSender(token, baseURL string) *sender {
	return &sender{
		token:   token,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetries: 2,
	}
}

func (s *sender) executeMessage(ctx context.Context, endpoint string, payload payload) (*message, error) {
	return nil, nil
}

func buildPayload(req hermes.MessageRequest) sendRequest {
	var endpoint string = fmt.Sprintf("channels/%s/messages", req.RecipientID)
	var payload payload

	if req.Text != "" {
		payload.Content = req.Text
	}

	if req.ReplyToID != "" {
		payload.MessageReference = req.ReplyToID
	}

	if len(req.Attachments) > 0 {
		payload.Embeds = mapEmbeds(req.Attachments)
	}

	sr := sendRequest{
		endpoint: endpoint,
		payload:  payload,
	}

	return sr
}
