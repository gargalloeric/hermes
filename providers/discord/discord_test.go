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

func TestDiscord_SendMessage(t *testing.T) {
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

func TestDiscord_Mapping(t *testing.T) {
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

func TestDiscord_ResolveAttachmentType(t *testing.T) {
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

func TestDiscord_Listen(t *testing.T) {
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

func TestDiscord_Gateway_Resumption(t *testing.T) {
	connCount := 0
	sessionID := "fake_session_123"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		connCount++
		ctx := r.Context()

		// 1. Send Hello
		hello, _ := json.Marshal(dsPayload{Op: dsOpHello, D: json.RawMessage(`{"heartbeat_interval": 45000}`)})
		conn.Write(ctx, websocket.MessageText, hello)

		// 2. Read Client Response
		_, data, _ := conn.Read(ctx)
		var p dsPayload
		json.Unmarshal(data, &p)

		if connCount == 1 {
			if p.Op != dsOpIdentify {
				t.Errorf("First connection should Identify, got %d", p.Op)
			}
			// Send READY to give the client a session ID
			ready, _ := json.Marshal(dsPayload{
				Op: dsOpDispatch, T: "READY",
				D: json.RawMessage(`{"session_id": "` + sessionID + `", "resume_gateway_url": "ws://..."}`),
			})
			conn.Write(ctx, websocket.MessageText, ready)
			// Simulate a server-side drop
			conn.Close(websocket.StatusNormalClosure, "dropping you")
		} else {
			if p.Op != dsOpResume {
				t.Errorf("Second connection should Resume, got %d", p.Op)
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	p := New("token")
	p.gatewayURL = "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	// We run Listen. It should connect, get dropped, and reconnect.
	out := make(chan *hermes.Message)
	go p.Listen(ctx, out)

	// Wait for the second connection to happen or timeout
	time.Sleep(500 * time.Millisecond)
}

func TestDiscord_SendMessage_RateLimitRetry(t *testing.T) {
	attempts := 0
	retryDuration := 100 * time.Millisecond

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// First attempt: Return 429 with a Retry-After header (seconds)
			w.Header().Set("Retry-After", "0.1")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"message":     "You are being rate limited.",
				"retry_after": 0.1,
				"code":        20000,
			})
			return
		}

		// Second attempt: Return 200 OK
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(dsResponse{
			dsMessage: dsMessage{
				ID:        "retry-success-id",
				ChannelID: "67890",
			},
		})
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	p := New("fake-token")
	p.baseURL = server.URL

	start := time.Now()
	msg, err := p.SendMessage(t.Context(), hermes.MessageRequest{
		RecipientID: "67890",
		Text:        "Testing retries",
	})

	if err != nil {
		t.Fatalf("SendMessage failed after retries: %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}

	if time.Since(start) < retryDuration {
		t.Errorf("test finished too fast (%v), retry logic might not be sleeping", time.Since(start))
	}

	if msg.ID != "retry-success-id" {
		t.Errorf("expected ID retry-success-id, got %s", msg.ID)
	}
}

func TestDiscord_SendMessage_Multipart(t *testing.T) {
	fileContent := []byte("fake-file-binary-data")
	fileHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fileContent)
	})
	fileServer := httptest.NewServer(fileHandler)
	defer fileServer.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the content type is multipart
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart content, got %s", r.Header.Get("Content-Type"))
		}

		// Parse the multipart form
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("failed to parse multipart form: %v", err)
		}

		// Verify payload_json field (Discord requirement)
		payloadJSON := r.FormValue("payload_json")
		if !strings.Contains(payloadJSON, "I have a file") {
			t.Errorf("payload_json missing text content, got: %s", payloadJSON)
		}

		// Verify the file part
		file, header, err := r.FormFile("files[0]")
		if err != nil {
			t.Fatalf("expected file in files[0], got error: %v", err)
		}
		defer file.Close()

		if header.Filename != "data.bin" {
			t.Errorf("expected filename data.bin, got %s", header.Filename)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(dsResponse{
			dsMessage: dsMessage{
				ID: "msg-with-file",
			},
		})
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	p := New("fake-token")
	p.baseURL = server.URL

	req := hermes.MessageRequest{
		RecipientID: "123",
		Text:        "I have a file",
		Attachments: []hermes.Attachment{
			{
				ID:       "att-1",
				URL:      fileServer.URL + "/data.bin", // Link to our local file server
				FileName: "data.bin",
				Type:     hermes.AttachmentFile, // Files trigger multipart, Images/Videos trigger Embeds
			},
		},
	}

	msg, err := p.SendMessage(t.Context(), req)
	if err != nil {
		t.Fatalf("SendMessage with multipart failed: %v", err)
	}

	if msg.ID != "msg-with-file" {
		t.Errorf("expected ID msg-with-file, got %s", msg.ID)
	}
}
