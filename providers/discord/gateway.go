package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/gargalloeric/hermes"
)

// Gateway OpCodes
const (
	dsOpDispatch       = 0  // Receive: An event was dispatched.
	dsOpHeartbeat      = 1  // Send/Reveive: Used for ping-pong.
	dsOpIdentify       = 2  // Send: Used for client handshake.
	dsOpResume         = 6  // Send: Attempt to resume a disconnected session
	dsOpReconnect      = 7  // Receive: Server is asking us to reconnect
	dsOpInvalidSession = 9  // Receive: Session is dead, must re-identify
	dsOpHello          = 10 // Receive: Sent by Discord immediately after connecting.
	dsOpHeartbeatAck   = 11 // Receive: Sent by Discord to acknowledge a heartbeat.
)

// Gateway Intents (Permissions)
const (
	dsIntentGuildMessages  = 1 << 9  // 512
	dsIntentMessageContent = 1 << 15 // 32768 (Note: v10 requires this for message text!)
)

type sessionState struct {
	backoff time.Duration

	mu        sync.RWMutex
	id        string
	resumeURL string
	sequence  atomic.Int64
}

func (s *sessionState) isBackoffUnder(d time.Duration) bool {
	return s.backoff < d
}

type gatewayManager struct {
	provider *Provider
	session  *sessionState
}

func (p *Provider) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	mgr := &gatewayManager{
		provider: p,
		session: &sessionState{
			backoff: 1 * time.Second,
		},
	}

	return mgr.run(ctx, out)
}

func (m *gatewayManager) run(ctx context.Context, out chan<- *hermes.Message) error {
	for {
		err := m.connectAndRead(ctx, out)

		// Exit if the parent context is cancelled
		if ctx.Err() != nil {
			return nil
		}

		// Handle reconnecting timing
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(m.session.backoff):
				if m.session.isBackoffUnder(60 * time.Second) {
					m.session.backoff *= 2
				}
			}
		} else {
			m.session.backoff = 1 * time.Second
		}
	}
}

func (m *gatewayManager) connectAndRead(ctx context.Context, out chan<- *hermes.Message) error {
	m.session.mu.RLock()
	dialURL := m.provider.gatewayURL
	if m.session.resumeURL != "" {
		dialURL = m.session.resumeURL
	}
	m.session.mu.RUnlock()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, _, err := websocket.Dial(connCtx, dialURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "reconnecting")

	for {
		_, data, err := conn.Read(connCtx)
		if err != nil {
			return err
		}

		var payload dsPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // Skip malformed payloads
		}

		if err := m.handleOP(connCtx, conn, payload, out); err != nil {
			return err
		}
	}

}

func (m *gatewayManager) handleOP(ctx context.Context, conn *websocket.Conn, payload dsPayload, out chan<- *hermes.Message) error {
	if payload.S != 0 {
		m.session.sequence.Store(payload.S)
	}

	switch payload.Op {
	case dsOpHello:
		var hello dsHello
		if err := json.Unmarshal(payload.D, &hello); err != nil {
			return fmt.Errorf("failed to parse hello: %w", err)
		}

		go m.provider.startHeartbeat(ctx, conn, &m.session.sequence, hello.HeartbeatInterval)

		m.session.mu.RLock()
		sid := m.session.id
		m.session.mu.RUnlock()

		if sid != "" {
			return m.resume(ctx, conn, sid, m.session.sequence.Load())
		}

		return m.identify(ctx, conn)

	case dsOpDispatch:
		return m.dispatch(payload, out)

	case dsOpReconnect:
		return fmt.Errorf("server requested reconnect")

	case dsOpInvalidSession:
		var resumable bool
		if err := json.Unmarshal(payload.D, &resumable); err != nil {
			return fmt.Errorf("failed to parse resumable: %w", err)
		}

		if !resumable {
			m.session.mu.Lock()
			m.session.id = ""
			m.session.mu.Unlock()
		}
		return fmt.Errorf("session invalid (resumable: %v)", resumable)

	case dsOpHeartbeat:
		return m.provider.sendHeartbeat(ctx, conn, &m.session.sequence)

	case dsOpHeartbeatAck:
		// Acknowledge! The connection is healthy
	}

	return nil
}

func (m *gatewayManager) resume(ctx context.Context, conn *websocket.Conn, sessionID string, seq int64) error {
	res := dsResume{
		Token:     m.provider.token,
		SessionID: sessionID,
		Seq:       seq,
	}

	return m.provider.writePayload(ctx, conn, dsOpResume, res)
}

func (m *gatewayManager) dispatch(payload dsPayload, out chan<- *hermes.Message) error {
	if payload.T == "READY" {
		var ready dsReady
		if err := json.Unmarshal(payload.D, &ready); err != nil {
			return fmt.Errorf("failed to parse ready: %w", err)
		}

		m.session.mu.Lock()
		m.session.id = ready.SessionID
		m.session.resumeURL = ready.ResumeURL
		m.session.mu.Unlock()

		return nil
	}

	if payload.T != "MESSAGE_CREATE" {
		return nil
	}

	var dsMsg dsMessage
	if err := json.Unmarshal(payload.D, &dsMsg); err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	hermesMsg := m.provider.mapToHermes(dsMsg)
	if hermesMsg != nil && !hermesMsg.Sender.IsBot {
		out <- hermesMsg
	}

	return nil
}

// identify sends the authentication payload to Discord.
func (m *gatewayManager) identify(ctx context.Context, conn *websocket.Conn) error {
	id := dsIdentity{
		Token:   m.provider.token,
		Intents: dsIntentGuildMessages | dsIntentMessageContent,
		Properties: dsIdentifyProperties{
			OS:      "linux",
			Browser: "hermes",
			Device:  "hermes",
		},
	}

	data, err := json.Marshal(id)
	if err != nil {
		return fmt.Errorf("failed to marshal gateway identity: %w", err)
	}

	payload, err := json.Marshal(dsPayload{Op: dsOpIdentify, D: data})
	if err != nil {
		return fmt.Errorf("failed to marshal gateway payload: %w", err)
	}

	return conn.Write(ctx, websocket.MessageText, payload)
}

// startHeartbeat manages the periodic pinging of the Discord Gateway.
func (p *Provider) startHeartbeat(ctx context.Context, conn *websocket.Conn, seq *atomic.Int64, intervalMs int) {
	interval := time.Duration(intervalMs) * time.Millisecond

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.sendHeartbeat(ctx, conn, seq); err != nil {
				return // Connection likely closed, the Read loop will handle the error
			}
		}
	}
}

func (p *Provider) sendHeartbeat(ctx context.Context, conn *websocket.Conn, seq *atomic.Int64) error {
	return p.writePayload(ctx, conn, dsOpHeartbeat, seq.Load())
}

// writePayload is a centralized helper to handle JSON encoding and WebSocket writes.
func (p *Provider) writePayload(ctx context.Context, conn *websocket.Conn, op int, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed marshaling data: %w", err)
	}

	payload := dsPayload{
		Op: op,
		D:  raw,
	}

	msg, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed marshaling payload: %w", err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return conn.Write(writeCtx, websocket.MessageText, msg)
}

func (p *Provider) mapToHermes(dsMsg dsMessage) *hermes.Message {
	m := &hermes.Message{
		ID:     dsMsg.ID,
		Text:   dsMsg.Content,
		ChatID: dsMsg.ChannelID,
		Sender: hermes.User{
			ID:       dsMsg.Author.ID,
			Username: dsMsg.Author.Username,
			IsBot:    dsMsg.Author.Bot,
		},
		Platform: p.Name(),
		Type:     hermes.TypeText,
	}

	if len(dsMsg.Attachments) > 0 {
		m.Attachments = p.mapAttachments(dsMsg.Attachments)

		for _, att := range m.Attachments {
			if att.Type != hermes.AttachmentFile {
				m.Type = hermes.MessageType(att.Type)
			}
		}
	}

	return m
}

func (p *Provider) mapAttachments(atts []dsAttachment) []hermes.Attachment {
	hermesAtts := make([]hermes.Attachment, 0, len(atts))

	for _, att := range atts {
		resolvedType := p.resolveAttachmentType(att.ContentType, att.Filename)

		hermesAtt := hermes.Attachment{
			ID:       att.ID,
			URL:      att.URL,
			FileName: att.Filename,
			Type:     resolvedType,
			MimeType: att.ContentType,
		}

		hermesAtts = append(hermesAtts, hermesAtt)
	}

	return hermesAtts
}

func (p *Provider) resolveAttachmentType(contentType, filename string) hermes.AttachmentType {
	if contentType != "" {
		if strings.HasPrefix(contentType, "image/") {
			return hermes.AttachmentImage
		}
		if strings.HasPrefix(contentType, "video/") {
			return hermes.AttachmentVideo
		}
		if strings.HasPrefix(contentType, "audio/") {
			return hermes.AttachmentAudio
		}
		return hermes.AttachmentFile
	}

	lowerName := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lowerName, ".png"), strings.HasSuffix(lowerName, ".jpg"),
		strings.HasSuffix(lowerName, ".jpeg"), strings.HasSuffix(lowerName, ".webp"),
		strings.HasSuffix(lowerName, ".gif"):
		return hermes.AttachmentImage
	case strings.HasSuffix(lowerName, ".mp4"), strings.HasSuffix(lowerName, ".mov"):
		return hermes.AttachmentVideo
	case strings.HasSuffix(lowerName, ".mp3"), strings.HasSuffix(lowerName, ".ogg"):
		return hermes.AttachmentAudio
	default:
		return hermes.AttachmentFile
	}
}
