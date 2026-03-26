package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gargalloeric/hermes"
)

const (
	baseURL    = "https://discord.com/api/v10"
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
)

// Provider implements the hermes.Provider interface for Discord.
type Provider struct {
	token  string
	client *http.Client
}

// New creates a new handcrafted Discord provider.
func New(token string) *Provider {
	return &Provider{
		token: "Bot " + token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "discord"
}

func (p *Provider) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	payload := map[string]any{
		"content": req.Text,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	url := fmt.Sprintf("%s/channels/%s/messages", baseURL, req.RecipientID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to construct discord request: %w", err)
	}

	httpReq.Header.Set("Authorization", p.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", hermes.UserAgent())

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("discord API request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := p.checkResponse(resp); err != nil {
		return nil, err
	}

	var dsResp dsMessage
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to decode discord responde: %w", err)
	}

	return &hermes.SentMessage{
		ID:     dsResp.ID,
		ChatID: dsResp.ChannelID,
	}, nil
}

func (p *Provider) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	var dsErr dsError
	if err := json.NewDecoder(resp.Body).Decode(&dsErr); err != nil {
		return fmt.Errorf("discord API returned status %d (could not decode error body)", resp.StatusCode)
	}

	dsErr.Status = resp.StatusCode

	return &dsErr
}
