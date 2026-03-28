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

// Provider implements the hermes.Provider interface for Discord.
type Provider struct {
	token      string
	client     *http.Client
	baseURL    string
	gatewayURL string
}

// New creates a new handcrafted Discord provider.
func New(token string) *Provider {
	return &Provider{
		token: "Bot " + token,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL:    "https://discord.com/api/v10",
		gatewayURL: "wss://gateway.discord.gg/?v=10&encoding=json",
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "discord"
}

func (p *Provider) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint, payload := p.buildPayload(req)

	dsResp, err := p.makeRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return nil, err
	}

	return &hermes.SentMessage{
		ID:       dsResp.ID,
		Platform: p.Name(),
		ChatID:   dsResp.ChannelID,
	}, nil
}

func (p *Provider) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint, payload := p.buildEditPayload(target, req)

	dsResp, err := p.makeRequest(ctx, http.MethodPatch, endpoint, payload)
	if err != nil {
		return nil, err
	}

	return &hermes.SentMessage{
		ID:       dsResp.ID,
		Platform: p.Name(),
		ChatID:   dsResp.ChannelID,
	}, nil
}

func (p *Provider) SendAction(ctx context.Context, req hermes.ActionRequest) error {
	action := p.mapAction(req.Action)
	endpoint := fmt.Sprintf("/channels/%s/%s", req.RecipientID, action)

	// Typing indicator doesn't need a body, so we pass nil.
	_, err := p.makeRequest(ctx, http.MethodPost, endpoint, nil)
	return err
}

func (p *Provider) ActionTimeout() time.Duration {
	return 10 * time.Second
}

func (p *Provider) mapAction(action hermes.ActionType) string {
	switch action {
	case hermes.ActionTyping:
		return "typing"
	default:
		return "typing"
	}
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

// buildPayload constructs the correct Discord endpoint and JSON payload.
func (p *Provider) buildPayload(req hermes.MessageRequest) (string, map[string]any) {
	endpoint := fmt.Sprintf("/channels/%s/messages", req.RecipientID)
	payload := make(map[string]any)

	if req.Text != "" {
		payload["content"] = req.Text
	}

	if req.ReplyToID != "" {
		payload["message_reference"] = map[string]string{
			"message_id": req.ReplyToID,
		}
	}

	if len(req.Attachments) > 0 {
		payload["embeds"] = p.mapAttachmentsToEmbeds(req.Attachments)
	}

	return endpoint, payload
}

func (p *Provider) buildEditPayload(target *hermes.SentMessage, req hermes.MessageRequest) (string, map[string]any) {
	endpoint := fmt.Sprintf("/channels/%s/messages/%s", target.ChatID, target.ID)
	payload := map[string]any{
		"content": req.Text,
	}

	// TODO: Discord allows to edit embeds also.
	return endpoint, payload
}

func (p *Provider) mapAttachmentsToEmbeds(atts []hermes.Attachment) []map[string]any {
	embeds := make([]map[string]any, 0, len(atts))

	for _, att := range atts {
		embed := map[string]any{
			"url":         att.URL,
			"title":       att.FileName,
			"description": att.MimeType,
		}

		switch att.Type {
		case hermes.AttachmentImage:
			embed["image"] = map[string]string{"url": att.URL}
		case hermes.AttachmentVideo:
			embed["video"] = map[string]string{"url": att.URL}
		}

		embeds = append(embeds, embed)
	}

	return embeds
}

func (p *Provider) makeRequest(ctx context.Context, method, endpoint string, payload any) (*dsMessage, error) {
	url := fmt.Sprintf("%s%s", p.baseURL, endpoint)

	req, err := p.newRequest(ctx, method, url, payload)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute send request: %w", err)
	}
	defer resp.Body.Close()

	if err := p.checkResponse(resp); err != nil {
		return nil, err
	}

	// Discord actions (e.g. typing) don't return a body.
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var dsResp dsMessage
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to decode discord response: %w", err)
	}

	return &dsResp, nil
}

func (p *Provider) newRequest(ctx context.Context, method, url string, payload any) (*http.Request, error) {
	var body []byte
	var err error

	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal discord payload: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create discord request: %w", err)
	}

	req.Header.Set("Authorization", p.token)
	req.Header.Set("User-Agent", hermes.UserAgent())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}
