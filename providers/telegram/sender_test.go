package telegram

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gargalloeric/hermes"
)

type mockTransport struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (mt *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return mt.roundTripFunc(req)
}

func TestSendMessage_Network(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		serverResponse string
		validate       func(*testing.T, *hermes.SentMessage, error)
	}{
		{
			name:           "Success Send",
			status:         http.StatusOK,
			serverResponse: `{"ok": true, "result": {"message_id": 777}}`,
			validate: func(t *testing.T, sm *hermes.SentMessage, err error) {
				if err != nil {
					t.Fatalf("did not expect error, got: %v", err)
				}
				if sm.ID != "777" {
					t.Errorf("expected ID 777, got %s", sm.ID)
				}
			},
		},
		{
			name:           "Telegram API Error 400",
			status:         http.StatusBadRequest,
			serverResponse: `{"ok": false, "error_code": 400, "description": "Bad Request: chat not found"}`,
			validate: func(t *testing.T, sm *hermes.SentMessage, err error) {
				if err == nil {
					t.Fatal("expected an error but got nil")
				}

				if !strings.Contains(err.Error(), "400") {
					t.Errorf("expected error to mention status 400, got: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.serverResponse))
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			p := NewPoller("fake-token")
			p.baseURL = server.URL + "/bot%s/%s"

			res, err := p.SendMessage(t.Context(), hermes.MessageRequest{Text: "Test"})

			tc.validate(t, res, err)
		})
	}
}

func TestEditMessage_Network(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botfake-token/editMessageText" {
			t.Errorf("expected editMessageText endpoint, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true, "result": {"message_id": 42}}`))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	p := NewPoller("fake-token")
	p.baseURL = server.URL + "/bot%s/%s"

	target := &hermes.SentMessage{ID: "42", ChatID: "123"}
	res, err := p.EditMessage(t.Context(), target, hermes.MessageRequest{Text: "Updated!"})

	if err != nil || res.ID != "42" {
		t.Errorf("EditMessage failed: %v", err)
	}
}

func TestSendMessage_Routing(t *testing.T) {
	tests := []struct {
		name             string
		req              hermes.MessageRequest
		expectedEndpoint string
	}{
		{
			name:             "Text only -> sendMessage",
			req:              hermes.MessageRequest{Text: "Hi"},
			expectedEndpoint: "/botfake-token/sendMessage",
		},
		{
			name: "Single Image -> sendPhoto",
			req: hermes.MessageRequest{
				Attachments: []hermes.Attachment{
					{Type: hermes.AttachmentImage, URL: "http://img.jpg"},
				},
			},
			expectedEndpoint: "/botfake-token/sendPhoto",
		},
		{
			name: "Multiple Attachments -> sendMediaGroup",
			req: hermes.MessageRequest{
				Attachments: []hermes.Attachment{
					{Type: hermes.AttachmentImage, URL: "1.jpg"},
					{Type: hermes.AttachmentImage, URL: "2.jpg"},
				},
			},
			expectedEndpoint: "/botfake-token/sendMediaGroup",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.expectedEndpoint {
					t.Errorf("expected %s, got %s", tc.expectedEndpoint, r.URL.Path)
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok": true, "result": {"message_id": 1}}`))
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			p := NewPoller("fake-token")
			p.baseURL = server.URL + "/bot%s/%s"
			p.SendMessage(t.Context(), tc.req)
		})
	}
}

func TestSendMessage_RateLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewPoller("fake-token")

		attempts := 0
		p.client.Transport = &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					// First attempt: Simulate Telegram rate limit (5 seconds)
					jsonPayload := `{"ok": false, "error_code": 429, "description": "Too Many Requests", "parameters": {"retry_after": 5}}`
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       io.NopCloser(strings.NewReader(jsonPayload)),
						Header:     make(http.Header),
					}, nil
				}

				// Second attempt: Success
				jsonPayload := `{"ok": true, "result": {"message_id": 999}}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(jsonPayload)),
					Header:     make(http.Header),
				}, nil
			},
		}

		// Capture the synthetic time before we call the method
		start := time.Now()

		req := hermes.MessageRequest{Text: "Testing smart retries"}
		res, err := p.SendMessage(t.Context(), req)

		if err != nil {
			t.Fatalf("expected success after retry, got error: %v", err)
		}

		if res.ID != "999" {
			t.Errorf("expected message ID 999, got %s", res.ID)
		}

		if attempts != 2 {
			t.Errorf("expected exactly 2 API calls, got %d", attempts)
		}

		elapsed := time.Since(start)
		if elapsed < 5*time.Second {
			t.Errorf("expected synthetic time to advance by at least 5s, got %v", elapsed)
		}
	})
}
