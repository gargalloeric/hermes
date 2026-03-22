package telegram

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gargalloeric/hermes"
)

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
			serverResponse: `{"ok": false, "description": "Bad Request: chat not found"}`,
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
