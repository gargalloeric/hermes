package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	return b
}

func TestGateway_dispatch(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*gateway, event)
		validation func(t *testing.T, g *gateway, err error)
	}

	tests := []testCase{
		{
			name: "READY event sets session ID and resume URL",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				r := ready{SessionID: "session_123", ResumeURL: "ws://resume"}
				e := event{
					Op: opDispatch,
					T:  eventReady,
					D:  mustMarshal(t, r),
				}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}

				g.session.mu.RLock()
				defer g.session.mu.RUnlock()
				if g.session.id != "session_123" {
					t.Errorf("expected session_id session_123, got %s", g.session.id)
				}
				if g.session.resumeURL != "ws://resume" {
					t.Errorf("expected resumeURL ws://resume, got %s", g.session.resumeURL)
				}
			},
		},
		{
			name: "MESSAGE_CREATE pushes to messages channel",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				// Buffer the channel to prevent deadlock in synchronous test
				g.messages = make(chan message, 1)

				msg := message{ID: "msg_1", Content: "Hello"}
				e := event{
					Op: opDispatch,
					T:  eventMessageCreate,
					D:  mustMarshal(t, msg),
				}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}

				select {
				case m := <-g.Messages():
					if m.ID != "msg_1" || m.Content != "Hello" {
						t.Errorf("unexpected message payload: %+v", m)
					}
				default:
					t.Fatal("expected message in channel, got none")
				}
			},
		},
		{
			name: "Unknown event types are ignored gracefully",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				e := event{
					Op: opDispatch,
					T:  "GUILD_CREATE",
					D:  []byte(`{}`),
				}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err != nil {
					t.Errorf("expected nil error for ignored event, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, e := tt.setup(t)
			err := g.dispatch(e)
			tt.validation(t, g, err)
		})
	}
}

func TestGateway_handle(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*gateway, event)
		validation func(t *testing.T, g *gateway, err error)
	}

	tests := []testCase{
		{
			name: "Updates sequence number on valid event",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				e := event{Op: opHeartbeatAck, S: 42}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if g.session.sequence.Load() != 42 {
					t.Errorf("expected sequence 42, got %d", g.session.sequence.Load())
				}
			},
		},
		{
			name: "opHeartbeatAck sets ack to true",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				g.session.ack.Store(false) // simulate waiting for ack
				e := event{Op: opHeartbeatAck}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				if !g.session.ack.Load() {
					t.Error("expected ack to be true")
				}
			},
		},
		{
			name: "opReconnect returns explicit error",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				e := event{Op: opReconnect}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err == nil || err.Error() != "server requested reconnect" {
					t.Errorf("expected reconnect error, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, e := tt.setup(t)
			err := g.handle(t.Context(), nil, e)
			tt.validation(t, g, err)
		})
	}
}

func TestGateway_invalidSession(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (*gateway, event)
		validation func(t *testing.T, g *gateway, err error)
	}

	tests := []testCase{
		{
			name: "Resumable session preserves ID",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				g.session.id = "active_session"
				e := event{D: []byte(`true`)}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err == nil {
					t.Error("expected error for invalid session")
				}
				if g.session.id != "active_session" {
					t.Errorf("expected session ID to be preserved, got %s", g.session.id)
				}
			},
		},
		{
			name: "Non-resumable session clears ID",
			setup: func(t *testing.T) (*gateway, event) {
				g := newGateway("token", "ws://dummy")
				g.session.id = "dead_session"
				e := event{D: []byte(`false`)}
				return g, e
			},
			validation: func(t *testing.T, g *gateway, err error) {
				if err == nil {
					t.Error("expected error for invalid session")
				}
				if g.session.id != "" {
					t.Errorf("expected session ID to be cleared, got %s", g.session.id)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, e := tt.setup(t)
			err := g.invalidSession(e)
			tt.validation(t, g, err)
		})
	}
}

func TestGateway_Flow(t *testing.T) {
	discord := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("server accept error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()

		// Step A: Send opHello immediately upon connection
		helloEvnt := event{
			Op: opHello,
			D:  mustMarshal(t, hello{HeartbeatInterval: 5000}),
		}
		helloData, _ := json.Marshal(helloEvnt)
		_ = conn.Write(ctx, websocket.MessageText, helloData)

		// Step B: Expect opIdentify from client
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Logf("server read error: %v", err)
			return
		}

		var clientEvent event
		_ = json.Unmarshal(data, &clientEvent)
		if clientEvent.Op != opIdentify {
			t.Errorf("expected opIdentify (2), got %d", clientEvent.Op)
			return
		}

		// Step C: Send opDispatch(READY) to simulate accepted identity
		readyEvnt := event{
			Op: opDispatch,
			T:  eventReady,
			D:  mustMarshal(t, ready{SessionID: "mock_session"}),
		}
		readyData, _ := json.Marshal(readyEvnt)
		_ = conn.Write(ctx, websocket.MessageText, readyData)

		// Let the test finish by holding the connection slightly
		<-ctx.Done()
	})

	server := httptest.NewServer(discord)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	g := newGateway("test_token", wsURL)

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- g.Start(ctx)
	}()

	// We wait for the context to cancel, meaning the handshake succeeded
	// and the gateway is idling/heartbeating.
	<-ctx.Done()

	// Ensure Start exited cleanly based on context cancellation
	err := <-errCh
	if err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	g.session.mu.RLock()
	defer g.session.mu.RUnlock()
	if g.session.id != "mock_session" {
		t.Errorf("Gateway failed to record session ID from integration flow. Got: %v", g.session.id)
	}
}
