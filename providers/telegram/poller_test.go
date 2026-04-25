package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockTransport allows to intercept HTTP requests and return fake responses.
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestTelegram_getUpdates(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*poller, context.Context)
		validation func(t *testing.T, updates []update, err error)
	}

	tests := []testCase{
		{
			name: "Successful API response",
			setup: func(t *testing.T) (*poller, context.Context) {
				p := newPoller("secret", "https://test")
				p.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						jsonResp := `{"ok":true,"result":[{"update_id":123}]}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(bytes.NewBufferString(jsonResp)),
						}, nil
					},
				}
				return p, t.Context()
			},
			validation: func(t *testing.T, updates []update, err error) {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if len(updates) != 1 || updates[0].UpdateID != 123 {
					t.Errorf("unexpected updates parsed: %+v", updates)
				}
			},
		},
		{
			name: "API returns OK=false with RetryAfter",
			setup: func(t *testing.T) (*poller, context.Context) {
				p := newPoller("secret", "https://test")
				p.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						jsonResp := `{"ok":false,"description":"Flood control","parameters":{"retry_after":30}}`
						return &http.Response{
							StatusCode: http.StatusTooManyRequests,
							Body:       io.NopCloser(bytes.NewBufferString(jsonResp)),
						}, nil
					},
				}
				return p, t.Context()
			},
			validation: func(t *testing.T, updates []update, err error) {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				apiErr, ok := errors.AsType[*apiError](err)
				if !ok {
					t.Fatalf("expected error to be *apiError, got %T", err)
				}
				if apiErr.RetryAfter != 30*time.Second || apiErr.Message != "Flood control" {
					t.Errorf("apiError fields mapped incorrectly: %+v", apiErr)
				}
			},
		},
		{
			name: "HTTP Transport error",
			setup: func(t *testing.T) (*poller, context.Context) {
				p := newPoller("secret", "https://test")
				p.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						return nil, errors.New("connection reset by peer")
					},
				}
				return p, t.Context()
			},
			validation: func(t *testing.T, updates []update, err error) {
				if err == nil {
					t.Fatal("expected error on transport failure")
				}
				if !strings.Contains(err.Error(), "connection reset by peer") {
					t.Errorf("unexpected error message: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ctx := tt.setup(t)
			updates, err := p.getUpdates(ctx)
			tt.validation(t, updates, err)
		})
	}
}

func TestTelegram_wait(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (context.Context, context.CancelFunc, time.Duration)
		validation func(t *testing.T, err error)
	}

	tests := []testCase{
		{
			name: "Wait 0 returns immediately with nil",
			setup: func(t *testing.T) (context.Context, context.CancelFunc, time.Duration) {
				return t.Context(), func() {}, 0
			},
			validation: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil for 0 delay, got %v", err)
				}
			},
		},
		{
			name: "Wait successfully passes duration",
			setup: func(t *testing.T) (context.Context, context.CancelFunc, time.Duration) {
				return t.Context(), func() {}, 1 * time.Millisecond
			},
			validation: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil after wait, got %v", err)
				}
			},
		},
		{
			name: "Context cancelled during wait",
			setup: func(t *testing.T) (context.Context, context.CancelFunc, time.Duration) {
				// Wrap t.Context() so we can intentionally cancel it early
				ctx, cancel := context.WithCancel(t.Context())
				cancel() // Cancel immediately
				return ctx, cancel, 5 * time.Second
			},
			validation: func(t *testing.T, err error) {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, delay := tt.setup(t)
			defer cancel() // Safe to call even on empty func(){}
			err := wait(ctx, delay)
			tt.validation(t, err)
		})
	}
}

func TestTelegram_Start(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*poller, context.Context, context.CancelFunc)
		validation func(t *testing.T, p *poller, err error)
	}

	tests := []testCase{
		{
			name: "Start stops when context is cancelled",
			setup: func(t *testing.T) (*poller, context.Context, context.CancelFunc) {
				p := newPoller("secret", "https://test")
				p.client.Transport = &mockTransport{
					roundTripFunc: func(req *http.Request) (*http.Response, error) {
						jsonResp := `{"ok":true,"result":[]}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(bytes.NewBufferString(jsonResp)),
						}, nil
					},
				}

				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return p, ctx, cancel
			},
			validation: func(t *testing.T, p *poller, err error) {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled error, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ctx, cancel := tt.setup(t)
			defer cancel()

			err := p.Start(ctx)

			tt.validation(t, p, err)
		})
	}
}
