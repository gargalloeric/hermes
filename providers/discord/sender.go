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
	for i := 0; i < s.maxRetries; i++ {
		var body io.Reader
		var contentType string = "application/json"

		if len(atts) > 0 {
			stream, cType := buildMultipartStream(ctx, s, payload, atts)
			body = stream
			contentType = cType
		} else {
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal JSON payload: %w", err)
			}
			body = bytes.NewReader(payloadBytes)
		}

		dResp, err := makeRequest(ctx, s, endpoint, method, contentType, body)

		if closer, ok := body.(io.ReadCloser); ok {
			closer.Close()
		}

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

		return &dResp.message, nil
	}

	return nil, fmt.Errorf("failed to send message after %d attempts", s.maxRetries)
}

func (s *sender) downloadFile(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build download file request for '%s': %w", url, err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send download request for '%s': %w", url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code %d downloading file '%s': %w", resp.StatusCode, url, err)
	}

	return resp, nil
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

func makeRequest(ctx context.Context, s *sender, endpoint, method, contentType string, body io.Reader) (*response, error) {
	url := fmt.Sprintf("%s%s", s.baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", s.token)
	req.Header.Set("User-Agent", hermes.UserAgent())

	if contentType != "" {
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

func buildMultipartStream(ctx context.Context, s *sender, payload payload, atts []hermes.Attachment) (io.ReadCloser, string) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer writer.Close()

		payloadJson, err := json.Marshal(payload)
		if err != nil {
			marshalError := fmt.Errorf("failed to marshal payload: %w", err)
			pw.CloseWithError(marshalError)
			return
		}

		pJson, err := writer.CreateFormField("payload_json")
		if err != nil {
			fieldError := fmt.Errorf("failed to create form field: %w", err)
			pw.CloseWithError(fieldError)
		}

		_, err = pJson.Write(payloadJson)
		if err != nil {
			writeError := fmt.Errorf("failed to write data to 'payload_json' field: %w", err)
			pw.CloseWithError(writeError)
			return
		}

		for i, att := range atts {
			if err := ctx.Err(); err != nil {
				pw.CloseWithError(err)
				return
			}

			fileName := fmt.Sprintf("files[%d]", i)
			part, err := writer.CreateFormFile(fileName, att.FileName)
			if err != nil {
				fieldError := fmt.Errorf("failed to create form file: %w", err)
				pw.CloseWithError(fieldError)
				return
			}

			resp, err := s.downloadFile(ctx, att.URL)
			if err != nil {
				resp.Body.Close()
				pw.CloseWithError(err)
				return
			}

			_, err = io.Copy(part, resp.Body)
			resp.Body.Close()

			if err != nil {
				copyError := fmt.Errorf("failed to copy file stream: %w", err)
				pw.CloseWithError(copyError)
				return
			}
		}
	}()

	return pr, writer.FormDataContentType()
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
