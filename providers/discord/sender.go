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

type sender struct {
	token      string
	baseURL    string
	client     *http.Client
	maxRetries int
}

type sendRequest struct {
	endpoint string
	payload  payload
	files    []hermes.Attachment
}

func newSender(token, baseURL string) *sender {
	return &sender{
		token:   "Bot " + token,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetries: 2,
	}
}

func (s *sender) executeMessage(ctx context.Context, endpoint, method string, payload payload, atts []hermes.Attachment) (*message, error) {
	var payloadBytes []byte
	contentType := "application/json"

	if len(atts) > 0 {
		files, err := fetchFiles(ctx, s, atts)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch attachments: %w", err)
		}

		encoded, err := encode(payload, files)
		if err != nil {
			return nil, fmt.Errorf("failed to encode multipart payload: %w", err)
		}

		payloadBytes = encoded.Bytes
		contentType = encoded.ContentType
	} else {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal the JSON payload: %w", err)
		}
	}

	dresp, err := executeWithRetry(ctx, s, endpoint, method, contentType, payloadBytes)
	if err != nil {
		return nil, err
	}

	return &dresp.message, nil
}

func (s *sender) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build download file request for '%s': %w", url, err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send download request for '%s': %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code %d downloading file '%s': %w", resp.StatusCode, url, err)
	}

	return io.ReadAll(resp.Body)
}

func buildPayload(req hermes.MessageRequest) sendRequest {
	var endpoint string = fmt.Sprintf("/channels/%s/messages", req.RecipientID)
	var payload payload
	var files []hermes.Attachment

	if req.Text != "" {
		payload.Content = req.Text
	}

	if req.ReplyToID != "" {
		payload.MessageReference = &messageReference{
			MessageID: req.ReplyToID,
		}
	}

	if len(req.Attachments) > 0 {
		payload.Embeds, files = mapMedia(req.Attachments)
	}

	sr := sendRequest{
		endpoint: endpoint,
		payload:  payload,
		files:    files,
	}

	return sr
}

func executeWithRetry(ctx context.Context, s *sender, endpoint, method, contentType string, payload []byte) (*response, error) {
	for range s.maxRetries {
		dResp, err := makeRequest(ctx, s, endpoint, method, contentType, payload)
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

		return dResp, nil
	}

	return nil, fmt.Errorf("failed to send message after %d retries", s.maxRetries)
}

func makeRequest(ctx context.Context, s *sender, endpoint, method, contentType string, payload []byte) (*response, error) {
	url := fmt.Sprintf("%s%s", s.baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", s.token)
	req.Header.Set("User-Agent", hermes.UserAgent())
	if len(payload) > 0 {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &response{Ok: true}, nil
	}

	var dResp response
	if err := json.NewDecoder(resp.Body).Decode(&dResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	dResp.Ok = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !dResp.Ok {
		return nil, wrapError(resp, &dResp)
	}

	return &dResp, nil
}

func fetchFiles(ctx context.Context, s *sender, atts []hermes.Attachment) ([]file, error) {
	files := make([]file, len(atts))
	for i, att := range atts {
		content, err := s.downloadFile(ctx, att.URL)
		if err != nil {
			// TODO: Improve strategy for a failed file download. For now, if one fails an error will be returned.
			return nil, err
		}
		files[i] = file{
			Filename: att.FileName,
			Content:  content,
		}
	}
	return files, nil
}

type encodedData struct {
	Bytes       []byte
	ContentType string
}

// encode encodes the payload as a form data payload tailored for the Discord API.
func encode(payload payload, files []file) (*encodedData, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	var buff bytes.Buffer
	writer := multipart.NewWriter(&buff)

	pJson, err := writer.CreateFormField("payload_json")
	if err != nil {
		return nil, fmt.Errorf("failed to create 'payload_json' field: %w", err)
	}

	_, err = pJson.Write(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to write data to 'payload_json' field: %w", err)
	}

	for i, file := range files {
		fieldName := fmt.Sprintf("files[%d]", i)
		pFile, err := writer.CreateFormFile(fieldName, file.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to encode file %s: %w", file.Filename, err)
		}

		_, err = io.Copy(pFile, bytes.NewReader(file.Content))
		if err != nil {
			return nil, fmt.Errorf("failed to write file %s to field %s: %w", file.Filename, fieldName, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &encodedData{
		Bytes:       buff.Bytes(),
		ContentType: writer.FormDataContentType(),
	}, nil
}

// wrapError centralizes the logic for extracting rate limits and error messages.
func wrapError(resp *http.Response, body *response) error {
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
