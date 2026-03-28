package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gargalloeric/hermes"
)

// Provider implements the hermes.Provider interface for Discord.
type Provider struct {
	token      string
	client     *http.Client
	baseURL    string
	gatewayURL string
	maxRetries int
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
		maxRetries: 2,
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return "discord"
}

func (p *Provider) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint, payload := p.buildPayload(req)

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPost, endpoint, payload)
		if err != nil {
			dsError, ok := errors.AsType[*dsError](err)
			if ok && dsError.RetryAfter > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(dsError.RetryAfter):
					continue
				}
			}
			return nil, err
		}

		return &hermes.SentMessage{
			ID:       dsResp.ID,
			Platform: p.Name(),
			ChatID:   dsResp.ChannelID,
		}, nil
	}

	return nil, fmt.Errorf("failed to send message after %d retries", p.maxRetries)
}

func (p *Provider) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint, payload := p.buildEditPayload(target, req)

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPatch, endpoint, payload)
		if err != nil {
			dsError, ok := errors.AsType[*dsError](err)
			if ok && dsError.RetryAfter > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(dsError.RetryAfter):
					continue
				}
			}
			return nil, err
		}

		return &hermes.SentMessage{
			ID:       dsResp.ID,
			Platform: p.Name(),
			ChatID:   dsResp.ChannelID,
		}, nil
	}
	return nil, fmt.Errorf("failed to edit message after %d retries", p.maxRetries)
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

func (p *Provider) makeRequest(ctx context.Context, method, endpoint string, payload any) (*dsResponse, error) {
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

	if resp.StatusCode == http.StatusNoContent {
		return &dsResponse{Ok: true}, nil
	}

	var dsResp dsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dsResp); err != nil {
		return nil, fmt.Errorf("failed to decode discord response: %w", err)
	}

	dsResp.Ok = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !dsResp.Ok {
		return nil, p.wrapError(resp, &dsResp)
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

// wrapError centralizes the logic for extracting rate limits and error messages.
func (p *Provider) wrapError(resp *http.Response, body *dsResponse) error {
	retry := body.RetryAfter

	// Discord header takes priority over the body for rate limits
	if h := resp.Header.Get("Retry-After"); h != "" {
		if val, err := strconv.ParseFloat(h, 64); err == nil {
			retry = val
		}
	}

	return &dsError{
		Code:       body.ErrorCode,
		Message:    body.Description,
		RetryAfter: time.Duration(retry * float64(time.Second)),
	}
}
