package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gargalloeric/hermes"
)

type Poller struct {
	token  string
	client *http.Client
	offset int
}

func NewPoller(token string) *Poller {
	return &Poller{
		token: token,
		client: &http.Client{
			Timeout: 70 * time.Second, // Must be longer than the Telegram timeout
		},
	}
}

func (p *Poller) Name() string {
	return "telegram"
}

func (p *Poller) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	baseURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", p.token)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			url := fmt.Sprintf("%s?offset=%d&timeout=60", baseURL, p.offset)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create polling request: %w", err)
			}

			resp, err := p.client.Do(req)
			if err != nil {
				time.Sleep(2 * time.Second) // Network hiccup, back off safely
				continue
			}

			var tgResp tgResponse
			if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			for _, upd := range tgResp.Result {
				if msg := p.mapToHermes(upd); msg != nil {
					out <- msg
				}
				p.offset = upd.UpdateID + 1
			}
		}
	}
}

func (p *Poller) mapToHermes(u tgUpdate) *hermes.Message {
	// If the update doesn't contain a message (e.g., it's a poll or callback), skip it for now.
	if u.Message == nil {
		return nil
	}

	m := &hermes.Message{
		ID:       strconv.Itoa(u.Message.MessageID),
		Platform: p.Name(),
		Sender: hermes.User{
			ID:       strconv.FormatInt(u.Message.From.ID, 10),
			Username: u.Message.From.Username,
		},
		Text: u.Message.Text,
		Type: hermes.TypeText,
	}

	// Telegram quirk: If a message has an image, the text is moved to the 'Caption' field.
	if u.Message.Caption != "" {
		m.Text = u.Message.Caption
	}

	if len(u.Message.Photo) > 0 {
		m.Type = hermes.TypeImage
		// Telegram sends multiple sizes; the last one is always the highest resolution.
		largest := u.Message.Photo[len(u.Message.Photo)-1]

		m.Attachments = append(m.Attachments, hermes.Attachment{
			Type: hermes.AttachmentImage,
			ID:   largest.FileID,
		})
	}

	// TODO: Handle System Events (New members joining)
	// if len(u.Message.NewChatMembers) > 0 { ... }

	m.Metadata = map[string]any{
		"raw_update_id": u.UpdateID,
	}

	return m
}

func (p *Poller) SendMessage(ctx context.Context, req hermes.MessageRequest) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.token)

	payload := map[string]any{
		"chat_id": req.RecipientID,
		"text":    req.Text,
	}

	if req.ReplyToID != "" {
		payload["reply_to_message_id"] = req.ReplyToID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create send request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: status %d", resp.StatusCode)
	}

	return nil
}
