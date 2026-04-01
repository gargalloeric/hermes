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
	payload := p.buildPayload(req)

	var files []dsFile
	if len(payload.Files) > 0 {
		for _, att := range payload.Files {
			data, err := p.downloadFile(ctx, att.URL)
			if err != nil {
				return nil, err
			}
			files = append(files, dsFile{FileName: att.FileName, Data: data})
			files = append(files, dsFile{FileName: att.FileName, Data: data})
		}
	}

	encoded, err := p.encode(payload.Data, files)
	if err != nil {
		return nil, err
	}

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPost, payload.Endpoint, encoded.Bytes, encoded.ContentType)
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

	encoded, err := p.encode(payload, nil)
	if err != nil {
		return nil, err
	}

	for range p.maxRetries {
		dsResp, err := p.makeRequest(ctx, http.MethodPatch, endpoint, encoded.Bytes, encoded.ContentType)
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

type payload struct {
	Endpoint string
	Data     map[string]any
	Files    []hermes.Attachment
}

// buildPayload constructs the correct Discord endpoint and JSON payload.
func (p *Provider) buildPayload(req hermes.MessageRequest) payload {
	pd := payload{
		Endpoint: fmt.Sprintf("/channels/%s/messages", req.RecipientID),
		Data:     make(map[string]any),
	}

	if req.Text != "" {
		pd.Data["content"] = req.Text
	}

	if req.ReplyToID != "" {
		pd.Data["message_reference"] = map[string]string{
			"message_id": req.ReplyToID,
		}
	}

	if len(req.Attachments) > 0 {
		embeds, files := p.splitAttachments(req.Attachments)

		if len(embeds) > 0 {
			pd.Data["embeds"] = embeds
		}

		pd.Files = files
	}

	return pd
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

type encodedData struct {
	Bytes       []byte
	ContentType string
}

func (p *Provider) encode(payload map[string]any, files []dsFile) (encodedData, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return encodedData{}, fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	if len(files) == 0 {
		return encodedData{
			Bytes:       payloadBytes,
			ContentType: "application/json",
		}, nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	defer writer.Close()

	pJson, err := writer.CreateFormField("payload_json")
	if err != nil {
		return encodedData{}, fmt.Errorf("failed to create payload_json field: %w", err)
	}

	_, err = pJson.Write(payloadBytes)
	if err != nil {
		return encodedData{}, fmt.Errorf("failed to write data to payload_json field: %w", err)
	}

	for i, file := range files {
		fieldName := fmt.Sprintf("files[%d]", i)
		pFile, err := writer.CreateFormFile(fieldName, file.FileName)
		if err != nil {
			return encodedData{}, fmt.Errorf("failed to encode file %s: %w", file.FileName, err)
		}

		_, err = io.Copy(pFile, bytes.NewReader(file.Data))
		if err != nil {
			return encodedData{}, fmt.Errorf("failed to encode file %s: %w", file.FileName, err)
		}
	}

	if err := writer.Close(); err != nil {
		return encodedData{}, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return encodedData{
		Bytes:       body.Bytes(),
		ContentType: writer.FormDataContentType(),
	}, nil
}

func (p *Provider) buildEditPayload(target *hermes.SentMessage, req hermes.MessageRequest) (string, map[string]any) {
	endpoint := fmt.Sprintf("/channels/%s/messages/%s", target.ChatID, target.ID)
	payload := map[string]any{
		"content": req.Text,
	}

	// TODO: Discord allows to edit embeds also.
	return endpoint, payload
}

// splitAttachments splits the attachments in two groups, the attachments that can be send as embeds to the Discord API
// and the attachments that need to be uploaded as multipart/form-data.
func (p *Provider) splitAttachments(atts []hermes.Attachment) ([]map[string]any, []hermes.Attachment) {
	var embeds []map[string]any
	var files []hermes.Attachment

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
			files = append(files, att)
		}
	}

	return embeds, files
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
