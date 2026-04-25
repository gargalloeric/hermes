package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Gateway Intents (Permissions)
// more information in https://docs.discord.com/developers/events/gateway#gateway-intents
const (
	intentGuildMessages  = 1 << 9
	intentMessageContent = 1 << 15
)

// Gateway OpCodes
const (
	opDispatch       = 0  // Receive: An event was dispatched.
	opHeartbeat      = 1  // Send/Reveive: Used for ping-pong.
	opIdentify       = 2  // Send: Used for client handshake.
	opResume         = 6  // Send: Attempt to resume a disconnected session
	opReconnect      = 7  // Receive: Server is asking us to reconnect
	opInvalidSession = 9  // Receive: Session is dead, must re-identify
	opHello          = 10 // Receive: Sent by Discord immediately after connecting.
	opHeartbeatAck   = 11 // Receive: Sent by Discord to acknowledge a heartbeat.
)

// Events
const (
	eventReady         = "READY"
	eventMessageCreate = "MESSAGE_CREATE"
)

type gatewaySession struct {
	sequence atomic.Int64

	mu        sync.RWMutex
	id        string
	resumeURL string
}

type gateway struct {
	token    string
	url      string
	messages chan message

	session gatewaySession
}

func newGateway(token string, url string) *gateway {
	return &gateway{
		token:    token,
		url:      url,
		messages: make(chan message),
	}
}

func (g *gateway) Start(ctx context.Context) error {
	defer close(g.messages)

	g.session.mu.RLock()
	dialURL := g.url
	if g.session.resumeURL != "" {
		dialURL = g.session.resumeURL
	}
	g.session.mu.RUnlock()

	conn, _, err := websocket.Dial(ctx, dialURL, nil)
	if err != nil {
		return fmt.Errorf("failed gateway dialing: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("failed reading from ws connection: %w", err)
		}

		var event event
		if err := json.Unmarshal(data, &event); err != nil {
			continue // Skipe malformed payloads
		}

		if err := g.handle(ctx, conn, event); err != nil {
			return err
		}

	}
}

func (g *gateway) Messages() <-chan message {
	return g.messages
}

func (g *gateway) handle(ctx context.Context, conn *websocket.Conn, event event) error {
	if event.S != 0 {
		g.session.sequence.Store(event.S)
	}

	switch event.Op {
	case opHello:
		var h hello
		if err := json.Unmarshal(event.D, &h); err != nil {
			return fmt.Errorf("failed to parse hello message: %w", err)
		}

		go g.startHeartbeat(ctx, conn, h.HeartbeatInterval)

		g.session.mu.RLock()
		id := g.session.id
		g.session.mu.RUnlock()

		if id != "" {
			return g.resume(ctx, conn, id)
		}

		return g.identify(ctx, conn)

	case opDispatch:
		return g.dispatch(event)
	case opReconnect:
		// TODO: Handle reconnect op
	case opInvalidSession:
		// TODO: Handle invalid session op
	case opHeartbeat:
		// TODO: Handle heartbeat op
	case opHeartbeatAck:
		// TODO: Handle heartbeat ack op
	}

	return nil
}

func (g *gateway) startHeartbeat(ctx context.Context, conn *websocket.Conn, interval int) {
	intervalMs := time.Duration(interval) * time.Millisecond

	ticker := time.NewTicker(intervalMs)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := write(ctx, conn, opHeartbeat, g.session.sequence.Load()); err != nil {
				return // Connection likely closed, the Read loop will handle the error
			}
		}
	}
}

func (g *gateway) identify(ctx context.Context, conn *websocket.Conn) error {
	identity := identify{
		Token:   g.token,
		Intents: intentGuildMessages | intentMessageContent,
		Properties: identifyProperties{
			OS:      "linux",
			Browser: "hermes",
			Device:  "hermes",
		},
	}

	return write(ctx, conn, opIdentify, identity)
}

func (g *gateway) resume(ctx context.Context, conn *websocket.Conn, sid string) error {
	resume := resume{
		Token:     g.token,
		SessionID: sid,
		Seq:       g.session.sequence.Load(),
	}

	return write(ctx, conn, opResume, resume)
}

func (g *gateway) dispatch(event event) error {
	if event.T == eventReady {
		var r ready
		if err := json.Unmarshal(event.D, &r); err != nil {
			return fmt.Errorf("failed to parse ready message: %w", err)
		}

		g.session.mu.Lock()
		g.session.id = r.SessionID
		g.session.resumeURL = r.ResumeURL
		g.session.mu.Unlock()

		return nil
	}

	if event.T == eventMessageCreate {
		return nil
	}

	var msg message
	if err := json.Unmarshal(event.D, &msg); err != nil {
		return fmt.Errorf("failed to parse content message: %w", err)
	}

	g.messages <- msg

	return nil
}

func write(ctx context.Context, conn *websocket.Conn, op int, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed marshaling data: %w", err)
	}

	event := event{
		Op: op,
		D:  raw,
	}
	msg, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event message: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return conn.Write(writeCtx, websocket.MessageText, msg)

}
