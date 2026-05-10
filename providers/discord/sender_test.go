package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gargalloeric/hermes"
)

// mockTransport allows to intercept HTTP requests and return custom responses.
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestSender_newSender(t *testing.T) {
	type testCase struct {
		name       string
		setup      func() (string, string)
		validation func(t *testing.T, s *sender)
	}

	tests := []testCase{
		{
			name: "Initializes with proper defaults and auth prefix",
			setup: func() (string, string) {
				return "secret123", "https://discord.com/api/v10"
			},
			validation: func(t *testing.T, s *sender) {
				if s.token != "Bot secret123" {
					t.Errorf("expected token to have Bot prefix, got %s", s.token)
				}
				if s.baseURL != "https://discord.com/api/v10" {
					t.Errorf("expected baseURL https://discord.com/api/v10, got %s", s.baseURL)
				}
				if s.client.Timeout != 10*time.Second {
					t.Errorf("expected 10s timeout, got %v", s.client.Timeout)
				}
				if s.maxRetries != 2 {
					t.Errorf("expected 2 max retries, got %d", s.maxRetries)
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

func TestSender_buildPayload(t *testing.T) {
	type testCase struct {
		name       string
		setup      func() hermes.MessageRequest
		validation func(t *testing.T, got sendRequest)
	}

	tests := []testCase{
		{
			name: "Simple text message",
			setup: func() hermes.MessageRequest {
				return hermes.MessageRequest{
					RecipientID: "chan_99",
					Text:        "Hello Discord",
				}
			},
			validation: func(t *testing.T, got sendRequest) {
				if got.endpoint != "/channels/chan_99/messages" {
					t.Errorf("incorrect endpoint: %s", got.endpoint)
				}
				if got.payload.Content != "Hello Discord" {
					t.Errorf("expected text 'Hello Discord', got '%s'", got.payload.Content)
				}
			},
		},
		{
			name: "Reply to message",
			setup: func() hermes.MessageRequest {
				return hermes.MessageRequest{
					RecipientID: "chan_99",
					Text:        "Reply text",
					ReplyToID:   "msg_123",
				}
			},
			validation: func(t *testing.T, got sendRequest) {
				if got.payload.MessageReference == nil || got.payload.MessageReference.MessageID != "msg_123" {
					t.Error("message reference mapping failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setup()
			got := buildPayload(req)
			tt.validation(t, got)
		})
	}
}

func TestSender_wrapError(t *testing.T) {
	type testCase struct {
		name       string
		setup      func() (*http.Response, *response)
		validation func(t *testing.T, err error)
	}

	tests := []testCase{
		{
			name: "Uses header priority for Rate Limits",
			setup: func() (*http.Response, *response) {
				resp := &http.Response{Header: make(http.Header)}
				resp.Header.Set("Retry-After", "1.5") // 1.5 seconds

				body := &response{
					ErrorCode:   50000,
					Description: "Rate limited",
					RetryAfter:  5.0, // Should be ignored in favor of header
				}
				return resp, body
			},
			validation: func(t *testing.T, err error) {
				var dErr *dsError
				if !errors.As(err, &dErr) {
					t.Fatalf("expected *dsError, got %T", err)
				}
				if dErr.RetryAfter != 1500*time.Millisecond {
					t.Errorf("expected 1.5s retry, got %v", dErr.RetryAfter)
				}
				if dErr.Code != 50000 {
					t.Errorf("expected code 50000, got %d", dErr.Code)
				}
			},
		},
		{
			name: "Falls back to body if header is missing",
			setup: func() (*http.Response, *response) {
				resp := &http.Response{Header: make(http.Header)}
				body := &response{
					RetryAfter: 0.5, // 0.5 seconds
				}
				return resp, body
			},
			validation: func(t *testing.T, err error) {
				var dErr *dsError
				if !errors.As(err, &dErr) {
					t.Fatalf("expected *dsError, got %T", err)
				}
				if dErr.RetryAfter != 500*time.Millisecond {
					t.Errorf("expected 500ms retry, got %v", dErr.RetryAfter)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := tt.setup()
			err := wrapError(resp, body)
			tt.validation(t, err)
		})
	}
}

func TestSender_downloadFile(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/success.txt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file content"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	type testCase struct {
		name       string
		setup      func(t *testing.T) (*sender, string)
		validation func(t *testing.T, resp *http.Response, err error)
	}

	tests := []testCase{
		{
			name: "Successful download",
			setup: func(t *testing.T) (*sender, string) {
				return newSender("token", ""), server.URL + "/success.txt"
			},
			validation: func(t *testing.T, resp *http.Response, err error) {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				if string(body) != "file content" {
					t.Errorf("expected 'file content', got '%s'", string(body))
				}
			},
		},
		{
			name: "Fails on bad status code",
			setup: func(t *testing.T) (*sender, string) {
				return newSender("token", ""), server.URL + "/notfound.txt"
			},
			validation: func(t *testing.T, resp *http.Response, err error) {
				if err == nil {
					t.Fatal("expected error on 404, got nil")
				}
				if resp != nil {
					t.Error("expected response to be nil after error (body should be closed internally)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, url := tt.setup(t)
			resp, err := s.downloadFile(t.Context(), url)
			tt.validation(t, resp, err)
		})
	}
}

func TestSender_buildMultipartStream(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("dummy image bytes"))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	s := newSender("token", "")
	ctx := t.Context()

	p := payload{Content: "caption"}
	atts := []hermes.Attachment{
		{FileName: "test.png", URL: server.URL},
	}

	stream, contentType := buildMultipartStream(ctx, s, p, atts)
	defer stream.Close()

	if !strings.HasPrefix(contentType, "multipart/form-data") {
		t.Errorf("expected multipart content type, got %s", contentType)
	}

	// Read the stream entirely to prevent goroutine leak and check content
	bodyBytes, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}

	bodyStr := string(bodyBytes)

	if !strings.Contains(bodyStr, `name="payload_json"`) {
		t.Error("multipart body missing payload_json field")
	}
	if !strings.Contains(bodyStr, `{"content":"caption"}`) {
		t.Error("multipart body missing JSON content")
	}

	if !strings.Contains(bodyStr, `filename="test.png"`) {
		t.Error("multipart body missing file name definition")
	}
	if !strings.Contains(bodyStr, "dummy image bytes") {
		t.Error("multipart body missing actual file bytes")
	}
}

func TestSender_executeMessage(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) *sender
		validation func(t *testing.T, msg *message, err error)
	}

	tests := []testCase{
		{
			name: "Success on first attempt",
			setup: func(t *testing.T) *sender {
				s := newSender("token", "http://discord")
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						res := response{message: message{ID: "msg_ok"}, Ok: true}
						b, _ := json.Marshal(res)
						return &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBuffer(b)),
						}, nil
					},
				}
				return s
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				if msg.ID != "msg_ok" {
					t.Errorf("expected ID msg_ok, got %s", msg.ID)
				}
			},
		},
		{
			name: "Retries upon receiving a Rate Limit (dsError)",
			setup: func(t *testing.T) *sender {
				s := newSender("token", "http://discord")
				attempts := 0
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						attempts++
						if attempts == 1 {
							// Return Rate Limit on first try
							res := response{ErrorCode: 50000, Description: "Rate Limit"}
							b, _ := json.Marshal(res)
							hdr := make(http.Header)
							hdr.Set("Retry-After", "0.01") // Wait 10ms
							return &http.Response{
								StatusCode: 429,
								Header:     hdr,
								Body:       io.NopCloser(bytes.NewBuffer(b)),
							}, nil
						}
						// Return Success on second try
						res := response{message: message{ID: "msg_retry_ok"}, Ok: true}
						b, _ := json.Marshal(res)
						return &http.Response{
							StatusCode: 200,
							Body:       io.NopCloser(bytes.NewBuffer(b)),
						}, nil
					},
				}
				return s
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err != nil {
					t.Fatalf("expected success after retry, got %v", err)
				}
				if msg.ID != "msg_retry_ok" {
					t.Errorf("expected ID msg_retry_ok, got %s", msg.ID)
				}
			},
		},
		{
			name: "Fails after max retries",
			setup: func(t *testing.T) *sender {
				s := newSender("token", "http://discord")
				s.maxRetries = 2
				s.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						// Always fail
						res := response{ErrorCode: 10001, Description: "Unknown Error"}
						b, _ := json.Marshal(res)
						hdr := make(http.Header)
						hdr.Set("Retry-After", "0.01") // Keeping delay low for testing
						return &http.Response{
							StatusCode: 400,
							Header:     hdr,
							Body:       io.NopCloser(bytes.NewBuffer(b)),
						}, nil
					},
				}
				return s
			},
			validation: func(t *testing.T, msg *message, err error) {
				if err == nil {
					t.Fatal("expected failure after max retries, got nil error")
				}
				if !strings.Contains(err.Error(), "failed to send message after") {
					t.Errorf("unexpected error string: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.setup(t)
			msg, err := s.executeMessage(t.Context(), "/test", http.MethodPost, payload{}, nil)
			tt.validation(t, msg, err)
		})
	}
}
