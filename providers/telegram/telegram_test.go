package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gargalloeric/hermes"
)

func TestTelegram_MapToHermes(t *testing.T) {
	p := NewPoller("fake-token")

	tests := []struct {
		name     string
		input    tgUpdate
		validate func(*testing.T, *hermes.Message)
	}{
		{
			name: "Simple Text Message",
			input: tgUpdate{
				Message: &tgMessage{
					MessageID: 1,
					From:      tgUser{ID: 123, Username: "hermes"},
					Text:      "Hello",
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Text != "Hello" || m.Type != hermes.TypeText {
					t.Errorf("expected text 'Hello', got %s", m.Text)
				}
			},
		},
		{
			name: "Photo with Caption (Highest Res)",
			input: tgUpdate{
				Message: &tgMessage{
					Caption: "Look at this!",
					Photo: []tgPhotoSize{
						{FileID: "low-res"},
						{FileID: "high-res"},
					},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeImage || m.Text != "Look at this!" {
					t.Error("failed to map image or caption")
				}
				if m.Attachments[0].ID != "high-res" {
					t.Errorf("expected highest resolution 'high-res', got %s", m.Attachments[0].ID)
				}
			},
		},
		{
			name: "User Joined Event",
			input: tgUpdate{
				Message: &tgMessage{
					NewChatMembers: []tgUser{{ID: 456, Username: "Hermes"}},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeEvent || m.Event.Type != hermes.EventUserJoined {
					t.Error("failed to map join event")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := p.mapToHermes(tc.input)
			if msg == nil {
				t.Fatal("expected message, got nil!")
			}

			tc.validate(t, msg)
		})
	}
}

func TestTelegram_Polling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewPoller("fake-token")
		p.backoff = 5 * time.Second

		var wg sync.WaitGroup
		pollCount := 0

		p.client.Transport = &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				pollCount++
				if pollCount == 1 {
					// Return 1 message immediately
					json := `{"ok": true, "result": [{"update_id": 100, "message": {"text": "hi"}}]}`
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(strings.NewReader(json)),
					}, nil
				}
				// Subsequent calls block to simulate long polling
				<-req.Context().Done()
				return nil, req.Context().Err()
			},
		}

		out := make(chan *hermes.Message, 1)
		ctx, cancel := context.WithCancel(t.Context())

		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Listen(ctx, out)
		}()

		// synctest.Wait() will:
		// 1. Run the first poll.
		// 2. See the message is sent.
		// 3. See the Listen loop hit timer.Reset(0) and loop again.
		// 4. See the second poll block in the mockTransport.
		// 5. Unblock the test.
		synctest.Wait()

		msg := <-out
		if msg.Text != "hi" {
			t.Errorf("expected hi, got %s", msg.Text)
		}

		cancel()
		wg.Wait()
	})
}

func TestTelegram_SendMessage_CancelledDurationRateLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := NewPoller("fake-token")

		p.client.Transport = &mockTransport{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate a massive 60-second rate limit penalty
				jsonPayload := `{"ok": false, "error_code": 429, "description": "Too Many Requests", "parameters": {"retry_after": 60}}`
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(strings.NewReader(jsonPayload)),
					Header:     make(http.Header),
				}, nil
			},
		}

		ctx, cancel := context.WithCancel(t.Context())

		// Schedule the cancellation to happen exactly 1 synthetic second from now.
		// This simulates a system shutdown occurring while the goroutine is sleeping.
		time.AfterFunc(1*time.Second, cancel)

		start := time.Now()

		req := hermes.MessageRequest{Text: "Cancel me!"}
		_, err := p.SendMessage(ctx, req)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled error, got: %v", err)
		}

		// Validate that we only waited for 1 second, NOT 60 seconds.
		// If the context cancellation failed, this would be 60s.
		elapsed := time.Since(start)
		if elapsed != 1*time.Second {
			t.Errorf("expected synthetic time to advance exactly 1s, got %v", elapsed)
		}
	})
}

type mockTransport struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (mt *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return mt.roundTripFunc(req)
}

func TestTelegram_SendMessage_Network(t *testing.T) {
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

func TestTelegram_EditMessage_Network(t *testing.T) {
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

func TestTelegram_SendMessage_Routing(t *testing.T) {
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

func TestTelegram_SendMessage_RateLimit(t *testing.T) {
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
