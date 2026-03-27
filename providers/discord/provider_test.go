package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gargalloeric/hermes"
)

func TestProvider_SendMessage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization != "Bot fake-token" {
			t.Errorf("expected Bot auth header, got %s", authorization)
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Mock Discord response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dsMessage{
			ID:        "12345",
			ChannelID: "67890",
			Content:   "hello back",
			Author:    dsUser{ID: "bot-id", Username: "HermesBot", Bot: true},
		})
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	p := New("fake-token")
	p.baseURL = server.URL

	req := hermes.MessageRequest{
		RecipientID: "67890",
		Text:        "hello",
	}

	msg, err := p.SendMessage(t.Context(), req)
	if err != nil {
		t.Fatalf("SendingMessage failed: %v", msg)
	}

	if msg.ID != "12345" {
		t.Errorf("expected ID 12345, got %s", msg.ID)
	}
}

func TestProvider_Mapping(t *testing.T) {
	tests := []struct {
		name     string
		input    dsMessage
		validate func(t *testing.T, m *hermes.Message)
	}{
		{
			name: "plain text message from human",
			input: dsMessage{
				ID:        "msg_1",
				Content:   "Hello Hermes",
				ChannelID: "chan_123",
				Author:    dsUser{ID: "usr_456", Username: "test", Bot: false},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeText {
					t.Errorf("expected TypeText, got %s", m.Type)
				}

				if m.Sender.IsBot {
					t.Errorf("expected IsBot to be false")
				}

				if m.ChatID != "chan_123" {
					t.Errorf("expected ChatID chan_123, got %s", m.ChatID)
				}
			},
		},
		{
			name: "message with image upgrades type",
			input: dsMessage{
				ID:        "msg_2",
				Content:   "cool pic",
				ChannelID: "chan_123",
				Author:    dsUser{ID: "bot_1", Username: "OtherBot", Bot: true},
				Attachments: []dsAttachment{
					{ID: "att_1", Filename: "sunset.jpg", ContentType: "image/jpeg", URL: "http://cdn.com/1.jpg"},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeImage {
					t.Errorf("expected TypeImage due to attachment, got %v", m.Type)
				}
				if len(m.Attachments) != 1 {
					t.Fatalf("expected 1 attachment, got %d", len(m.Attachments))
				}
				if m.Attachments[0].Type != hermes.AttachmentImage {
					t.Errorf("expected AttachmentImage, got %v", m.Attachments[0].Type)
				}
				if !m.Sender.IsBot {
					t.Error("expected IsBot to be true")
				}
			},
		},
		{
			name: "multiple attachments first media wins type",
			input: dsMessage{
				ID:      "msg_3",
				Content: "files",
				Attachments: []dsAttachment{
					{ID: "a1", Filename: "doc.pdf", ContentType: "application/pdf"},
					{ID: "a2", Filename: "vid.mp4", ContentType: "video/mp4"},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				// The mapper should upgrade the message type to Video because it's higher priority than File
				if m.Type != hermes.TypeVideo {
					t.Errorf("expected TypeVideo, got %v", m.Type)
				}
				if len(m.Attachments) != 2 {
					t.Errorf("expected 2 attachments, got %d", len(m.Attachments))
				}
			},
		},
	}

	p := New("fake-token")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.mapToHermes(tc.input)

			tc.validate(t, got)
		})
	}

}

func TestProvider_ResolveAttachmentType(t *testing.T) {
	tests := []struct {
		name     string
		mime     string
		filename string
		validate func(t *testing.T, got hermes.AttachmentType)
	}{
		{
			name:     "MIME type priority: image",
			mime:     "image/webp",
			filename: "document.pdf", // Conflicting extension
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentImage {
					t.Errorf("MIME type 'image/webp' should take priority over .pdf extension, got %v", got)
				}
			},
		},
		{
			name:     "MIME type priority: video",
			mime:     "video/quicktime",
			filename: "clip.mov",
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentVideo {
					t.Errorf("expected AttachmentVideo, got %v", got)
				}
			},
		},
		{
			name:     "Fallback to extension: png",
			mime:     "",             // Missing MIME
			filename: "snapshot.PNG", // Uppercase check
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentImage {
					t.Errorf("expected extension .PNG to resolve to AttachmentImage, got %v", got)
				}
			},
		},
		{
			name:     "Fallback to extension: audio",
			mime:     "",
			filename: "podcast.ogg",
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentAudio {
					t.Errorf("expected .ogg to resolve to AttachmentAudio, got %v", got)
				}
			},
		},
		{
			name:     "Unknown types default to File",
			mime:     "application/octet-stream",
			filename: "data.bin",
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentFile {
					t.Errorf("unknown MIME and extension should be AttachmentFile, got %v", got)
				}
			},
		},
		{
			name:     "Empty input handling",
			mime:     "",
			filename: "",
			validate: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentFile {
					t.Errorf("empty inputs must default to AttachmentFile, got %v", got)
				}
			},
		},
	}

	p := New("fake-token")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.resolveAttachmentType(tc.mime, tc.filename)

			tc.validate(t, got)
		})
	}
}

func TestProvider_Listen(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("failed to accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx := r.Context()

		// A. Send "Hello" (Op 10)
		helloPayload, _ := json.Marshal(dsPayload{
			Op: dsOpHello,
			D:  json.RawMessage(`{"heartbeat_interval": 45000}`),
		})
		conn.Write(ctx, websocket.MessageText, helloPayload)

		// B. Wait for "Identify" (Op 2)
		_, identifyData, _ := conn.Read(ctx)
		var ident dsPayload
		json.Unmarshal(identifyData, &ident)
		if ident.Op != dsOpIdentify {
			t.Errorf("expected Identify (Op 2), got Op %d", ident.Op)
		}

		// C. Send a Mock Message Dispatch (Op 0)
		msgData, _ := json.Marshal(dsMessage{
			ID:        "999",
			Content:   "Gateway test",
			ChannelID: "ch_1",
			Author:    dsUser{ID: "u_1", Username: "test"},
		})
		dispatch, _ := json.Marshal(dsPayload{
			Op: dsOpDispatch,
			T:  "MESSAGE_CREATE",
			S:  1,
			D:  msgData,
		})
		conn.Write(ctx, websocket.MessageText, dispatch)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	p := New("fake-token")
	p.gatewayURL = "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	out := make(chan *hermes.Message, 1)
	errChan := make(chan error, 1)

	go func() {
		errChan <- p.Listen(ctx, out)
	}()

	// Assert we receive the dispatched message
	select {
	case msg := <-out:
		if msg.Text != "Gateway test" {
			t.Errorf("Expected 'Gateway test', got '%s'", msg.Text)
		}
	case err := <-errChan:
		t.Fatalf("Listen exited prematurely: %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for Gateway message")
	}

	// Verify clean shutdown
	cancel()
	if err := <-errChan; err != nil {
		t.Errorf("Listen returned error on shutdown: %v", err)
	}
}
