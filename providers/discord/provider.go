package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
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
	endpoint, payload, uploads := p.buildPayload(req)

	var files []dsFile
	if len(uploads) > 0 {
		for _, att := range uploads {
			data, err := p.downloadFile(ctx, att.URL)
			if err != nil {
				return nil, err
			}
			contentType := http.DetectContentType(data)
			files = append(files, dsFile{FileName: att.FileName, Data: data, ContentType: contentType})
		}
	}

	body, contentType, err := p.encode(payload, files)
	if err != nil {
		return nil, err
	}

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPost, endpoint, body, contentType)
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

	body, contentType, err := p.encode(payload, nil)
	if err != nil {
		return nil, err
	}

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPatch, endpoint, body, contentType)
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
	_, err := p.makeRequest(ctx, http.MethodPost, endpoint, nil, "")
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
func (p *Provider) buildPayload(req hermes.MessageRequest) (string, map[string]any, []hermes.Attachment) {
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
		embeds, uploads := p.splitAttachments(req.Attachments)

		if len(embeds) > 0 {
			payload["embeds"] = embeds
		}

		return endpoint, payload, uploads
	}

	return endpoint, payload, nil
}

func (p *Provider) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build download request for %s", url)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to url %s", url)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (p *Provider) encode(payload map[string]any, files []dsFile) ([]byte, string, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	if len(files) == 0 {
		return payloadBytes, "application/json", nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	defer writer.Close()

	pJson, err := writer.CreateFormField("payload_json")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create payload_json field: %w", err)
	}

	_, err = pJson.Write(payloadBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to write data to payload_json field: %w", err)
	}

	for i, file := range files {
		fieldName := fmt.Sprintf("files[%d]", i)
		pFile, err := writer.CreateFormFile(fieldName, file.FileName)
		if err != nil {
			return nil, "", fmt.Errorf("failed to encode file %s: %w", file.FileName, err)
		}

		_, err = io.Copy(pFile, bytes.NewReader(file.Data))
		if err != nil {
			return nil, "", fmt.Errorf("failed to encode file %s: %w", file.FileName, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func (p *Provider) buildEditPayload(target *hermes.SentMessage, req hermes.MessageRequest) (string, map[string]any) {
	endpoint := fmt.Sprintf("/channels/%s/messages/%s", target.ChatID, target.ID)
	payload := map[string]any{
		"content": req.Text,
	}

	// TODO: Discord allows to edit embeds also.
	return endpoint, payload
}

// splitAttachments splits the attachments in two gorups, the attachments that can be send as embeds to the Discord API
// and the attachments that need to be uploaded as multipart/form-data.
func (p *Provider) splitAttachments(atts []hermes.Attachment) ([]map[string]any, []hermes.Attachment) {
	var embeds []map[string]any
	var uploads []hermes.Attachment

	for _, att := range atts {
		switch att.Type {
		case hermes.AttachmentImage:
			embeds = append(embeds, map[string]any{
				"url":   att.URL,
				"title": att.FileName,
				"image": map[string]string{"url": att.URL},
			})
		case hermes.AttachmentVideo:
			embeds = append(embeds, map[string]any{
				"url":   att.URL,
				"title": att.FileName,
				"video": map[string]string{"url": att.URL},
			})
		default:
			att.FileName = path.Base(att.URL)
			uploads = append(uploads, att)
		}
	}

	return embeds, uploads
}

func (p *Provider) makeRequest(ctx context.Context, method, endpoint string, payload []byte, contentType string) (*dsResponse, error) {
	url := fmt.Sprintf("%s%s", p.baseURL, endpoint)

	req, err := p.newRequest(ctx, method, url, payload, contentType)
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

func (p *Provider) newRequest(ctx context.Context, method, url string, payload []byte, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create discord request: %w", err)
	}

	req.Header.Set("Authorization", p.token)
	req.Header.Set("User-Agent", hermes.UserAgent())
	if payload != nil {
		req.Header.Set("Content-Type", contentType)
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
