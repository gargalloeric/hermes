package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gargalloeric/hermes"
)

// sender sends messages to the Telegram API.
type sender struct {
	token      string
	client     *http.Client
	maxRetries int
}

type sendRequest struct {
	endpoint string
	payload  payload
}

func newSender(token string) *sender {
	return &sender{
		token: "bot" + token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		maxRetries: 2,
	}
}

func (s *sender) executeMessage(ctx context.Context, endpoint string, payload payload) (*message, error) {
	return executeWithRetry[*message](ctx, s, endpoint, payload)
}

func (s *sender) executeAction(ctx context.Context, endpoint string, payload payload) (bool, error) {
	return executeWithRetry[bool](ctx, s, endpoint, payload)
}

func executeWithRetry[T any](ctx context.Context, s *sender, endpoint string, payload payload) (T, error) {
	var result T
	var target string = fmt.Sprintf("%s/%s/%s", apiBase, s.token, endpoint)

	for range s.maxRetries {
		msg, err := makeRequest(ctx, s, target, payload)
		if err == nil {
			err = json.Unmarshal(msg.Result, &result)
			return result, err
		}

		apiErr, ok := errors.AsType[*apiError](err)
		if !ok || apiErr.RetryAfter <= 0 {
			return result, nil
		}

		if err := wait(ctx, apiErr.RetryAfter); err != nil {
			return result, err
		}
	}

	return result, fmt.Errorf("failed to send message after %d retries", s.maxRetries)
}

func makeRequest(ctx context.Context, s *sender, endpoint string, payload payload) (*postResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", hermes.UserAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform http request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp postResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !apiResp.Ok {
		return nil, wrapErrResponse(apiResp.Parameters, apiResp.Description)
	}

	return &apiResp, nil
}

func buildSendPayload(req hermes.MessageRequest) sendRequest {
	if len(req.Attachments) == 0 {
		return onlyText(req)
	}

	if len(req.Attachments) == 1 {
		return singleAttachment(req)
	}

	return multipleAttachments(req)
}

func onlyText(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
		Text:   req.Text,
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	sendReq := sendRequest{
		endpoint: "sendMessage",
		payload:  payload,
	}

	return sendReq
}

func singleAttachment(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
	}

	if req.Text != "" {
		payload.Caption = req.Text
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	att := req.Attachments[0]
	var endpoint string

	switch att.Type {
	case hermes.AttachmentImage:
		payload.Photo = att.URL
		endpoint = "sendPhoto"
	case hermes.AttachmentVideo:
		payload.Video = att.URL
		endpoint = "sendVideo"
	default:
		payload.Document = att.URL
		endpoint = "sendDocument"
	}

	sendReq := sendRequest{
		endpoint: endpoint,
		payload:  payload,
	}

	return sendReq
}

func multipleAttachments(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
		Media:  getMediaGroup(req),
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	sendReq := sendRequest{
		endpoint: "sendMediaGroup",
		payload:  payload,
	}

	return sendReq
}

func getMediaGroup(req hermes.MessageRequest) []payloadMedia {
	group := make([]payloadMedia, len(req.Attachments))

	for i, att := range req.Attachments {
		item := payloadMedia{
			Media: att.URL,
			Type:  getMediaType(att.Type),
		}

		if i == 0 && req.Text != "" {
			item.Caption = req.Text
		}

		group[i] = item
	}

	return group
}

func getMediaType(att hermes.AttachmentType) string {
	switch att {
	case hermes.AttachmentImage:
		return "photo"
	case hermes.AttachmentVideo:
		return "video"
	default:
		return "document"
	}
}
