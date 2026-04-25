package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gargalloeric/hermes"
)

func TestTelegram_newSender(t *testing.T) {
	type testCase struct {
		name       string
		setup      func() (string, string)
		validation func(t *testing.T, s *sender)
	}

	tests := []testCase{
		{
			name: "Sender initializes with correct defaults",
			setup: func() (string, string) {
				return "token123", "https://api.telegram.org"
			},
			validation: func(t *testing.T, s *sender) {
				if s.token != "bottoken123" {
					t.Errorf("expected bottoken123, got %s", s.token)
				}
				if s.client.Timeout != 30*time.Second {
					t.Errorf("expected 30s timeout, got %v", s.client.Timeout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, baseURL := tt.setup()
			got := newSender(token, baseURL)
			tt.validation(t, got)
		})
	}
}

func TestTelegram_buildSendPayload(t *testing.T) {
	type testCase struct {
		name       string
		setup      func() hermes.MessageRequest
		validation func(t *testing.T, got sendRequest)
	}

	tests := []testCase{
		{
			name: "Only text message",
			setup: func() hermes.MessageRequest {
				return hermes.MessageRequest{
					RecipientID: "123",
					Text:        "Hello",
					ReplyToID:   "456",
				}
			},
			validation: func(t *testing.T, got sendRequest) {
				if got.endpoint != "sendMessage" {
					t.Errorf("expected sendMessage, got %s", got.endpoint)
				}
				if got.payload.Text != "Hello" || got.payload.ReplyToMessageID != "456" {
					t.Errorf("payload mismatch: %+v", got.payload)
				}
			},
		},
		{
			name: "Single image attachment",
			setup: func() hermes.MessageRequest {
				return hermes.MessageRequest{
					RecipientID: "123",
					Text:        "Caption here",
					Attachments: []hermes.Attachment{{Type: hermes.AttachmentImage, URL: "http://img.jpg"}},
				}
			},
			validation: func(t *testing.T, got sendRequest) {
				if got.endpoint != "sendPhoto" {
					t.Errorf("expected sendPhoto, got %s", got.endpoint)
				}
				if got.payload.Photo != "http://img.jpg" || got.payload.Caption != "Caption here" {
					t.Errorf("payload mismatch: %+v", got.payload)
				}
			},
		},
		{
			name: "Multiple attachments (Media Group)",
			setup: func() hermes.MessageRequest {
				return hermes.MessageRequest{
					RecipientID: "123",
					Text:        "Album caption",
					Attachments: []hermes.Attachment{
						{Type: hermes.AttachmentImage, URL: "img1.jpg"},
						{Type: hermes.AttachmentVideo, URL: "vid1.mp4"},
					},
				}
			},
			validation: func(t *testing.T, got sendRequest) {
				if got.endpoint != "sendMediaGroup" {
					t.Errorf("expected sendMediaGroup, got %s", got.endpoint)
				}
				if len(got.payload.Media) != 2 {
					t.Fatalf("expected 2 media items, got %d", len(got.payload.Media))
				}
				// Verify caption is only on the first item
				if got.payload.Media[0].Caption != "Album caption" || got.payload.Media[1].Caption != "" {
					t.Error("caption should only be present on the first media item")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.setup()
			got := buildSendPayload(input)
			tt.validation(t, got)
		})
	}
}

func TestTelegram_executeWithRetry(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*sender, context.Context)
		validation func(t *testing.T, msg *message, err error)
	}

	tests := []testCase{
		{
			name: "Success on first attempt",
			setup: func(t *testing.T) (*sender, context.Context) {
				s := newSender("key", "http://test")
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						res := postResponse{Ok: true, Result: json.RawMessage(`{"message_id": 999}`)}
						body, _ := json.Marshal(res)
						return &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBuffer(body)),
						}, nil
					},
				}
				return s, t.Context()
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				if msg.MessageID != 999 {
					t.Errorf("expected message_id 999, got %d", msg.MessageID)
				}
			},
		},
		{
			name: "Success after one retry (Flood Control)",
			setup: func(t *testing.T) (*sender, context.Context) {
				s := newSender("key", "http://test")
				attempts := 0
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						attempts++
						if attempts == 1 {
							// Return RetryAfter error
							res := postResponse{
								Ok:          false,
								Description: "Too many requests",
								Parameters:  &parameters{RetryAfter: 1},
							}
							body, _ := json.Marshal(res)
							return &http.Response{
								StatusCode: 429,
								Body:       io.NopCloser(bytes.NewBuffer(body)),
							}, nil
						}
						// Success on second try
						res := postResponse{Ok: true, Result: json.RawMessage(`{"message_id": 100}`)}
						body, _ := json.Marshal(res)
						return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBuffer(body))}, nil
					},
				}
				return s, t.Context()
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err != nil {
					t.Fatalf("expected success after retry, got %v", err)
				}
				if msg.MessageID != 100 {
					t.Errorf("expected message_id 100, got %d", msg.MessageID)
				}
			},
		},
		{
			name: "Fails after max retries",
			setup: func(t *testing.T) (*sender, context.Context) {
				s := newSender("key", "http://test")
				s.maxRetries = 2
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						res := postResponse{
							Ok:          false,
							Description: "Still failing",
							Parameters:  &parameters{RetryAfter: 1},
						}
						body, _ := json.Marshal(res)
						return &http.Response{StatusCode: 429, Body: io.NopCloser(bytes.NewBuffer(body))}, nil
					},
				}
				return s, t.Context()
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err == nil {
					t.Fatal("expected error after max retries, got nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ctx := tt.setup(t)
			msg, err := s.executeMessage(ctx, "sendMessage", payload{})
			tt.validation(t, msg, err)
		})
	}
}

func TestTelegram_makeRequest(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*sender, context.Context, payload)
		validation func(t *testing.T, res *postResponse, err error)
	}

	tests := []testCase{
		{
			name: "API returns Error (Ok: false)",
			setup: func(t *testing.T) (*sender, context.Context, payload) {
				s := newSender("key", "http://test")
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						res := postResponse{Ok: false, Description: "Unauthorized"}
						body, _ := json.Marshal(res)
						return &http.Response{StatusCode: 401, Body: io.NopCloser(bytes.NewBuffer(body))}, nil
					},
				}
				return s, t.Context(), payload{Text: "test"}
			},
			validation: func(t *testing.T, res *postResponse, err error) {
				if err == nil {
					t.Fatal("expected error for Ok:false, got nil")
				}
				if _, ok := errors.AsType[*apiError](err); !ok {
					t.Errorf("expected *apiError, got %T", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ctx, p := tt.setup(t)
			res, err := makeRequest(ctx, s, "http://test", p)
			tt.validation(t, res, err)
		})
	}
}
