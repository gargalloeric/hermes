package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/gargalloeric/hermes"
)

// Gateway OpCodes
const (
	dsOpDispatch     = 0  // Receive: An event was dispatched.
	dsOpHeartbeat    = 1  // Send/Reveive: Used for ping-pong.
	dsOpIdentify     = 2  // Send: Used for client handshake.
	dsOpHello        = 10 // Receive: Sent by Discord immediately after connecting.
	dsOpHeartbeatAck = 11 // Receive: Sent by Discord to acknowledge a heartbeat.
)

// Gateway Intents (Permissions)
const (
	dsIntentGuildMessages  = 1 << 9  // 512
	dsIntentMessageContent = 1 << 15 // 32768 (Note: v10 requires this for message text!)
)

func (p *Provider) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	conn, _, err := websocket.Dial(ctx, gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("failed to dial discord gateway: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "hermes-session-end")

	var sequence atomic.Int64

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// If the context is cancelled, this isn't an "error" we want to bubble up as a failure.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("discord websocket read error: %w", err)
		}

		var payload dsPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // Skip malformed payloads
		}

		if payload.S != 0 {
			sequence.Store(payload.S)
		}

		switch payload.Op {
		case dsOpHello:
			var hello dsHello
			if err := json.Unmarshal(payload.D, &hello); err != nil {
				return fmt.Errorf("failed to parse hello: %w", err)
			}

			go p.startHeartbeat(ctx, conn, &sequence, hello.HeartbeatInterval)

			if err := p.identify(ctx, conn); err != nil {
				return fmt.Errorf("failed to identify: %w", err)
			}

		case dsOpDispatch:
			p.handleDispatch(payload, out)

		case dsOpHeartbeat:
			p.sendHeartbeat(ctx, conn, &sequence)

		case dsOpHeartbeatAck:
			// Acknowledged! The connection is healthy.
		}
	}
}

// handleDispatch separates the routing logic from the connection logic.
func (p *Provider) handleDispatch(payload dsPayload, out chan<- *hermes.Message) {
	if payload.T != "MESSAGE_CREATE" {
		return
	}

	var dsMsg dsMessage
	if err := json.Unmarshal(payload.D, &dsMsg); err != nil {
		return
	}

	if hermesMsg := p.mapToHermes(dsMsg); hermesMsg != nil {
		out <- hermesMsg
	}
}

// identify sends the authentication payload to Discord.
func (p *Provider) identify(ctx context.Context, conn *websocket.Conn) error {
	id := dsIdentity{
		Token:   p.token,
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
		m.Attachments = p.mapAttachments(m, dsMsg.Attachments)
	}

	return m
}

func (p *Provider) mapAttachments(m *hermes.Message, atts []dsAttachment) []hermes.Attachment {
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

		if m.Type == hermes.TypeText && resolvedType != hermes.AttachmentFile {
			m.Type = hermes.MessageType(resolvedType)
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
