package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gargalloeric/hermes"
)

func TestListen_Polling(t *testing.T) {
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

func TestSendMessage_CancellationDuringRateLimit(t *testing.T) {
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
