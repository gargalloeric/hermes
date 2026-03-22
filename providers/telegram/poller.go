package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
			url := fmt.Sprintf("%s?offset=%d&timeout=60allowed_updates=[\"message\",\"edited_message\",\"chat_member\"]", baseURL, p.offset)
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

	// Multimedia handling
	if len(u.Message.Photo) > 0 {
		m.Type = hermes.TypeImage
		// Telegram sends multiple sizes; the last one is always the highest resolution.
		largest := u.Message.Photo[len(u.Message.Photo)-1]

		m.Attachments = append(m.Attachments, hermes.Attachment{
			Type: hermes.AttachmentImage,
			ID:   largest.FileID,
		})
	} else if u.Message.Video != nil {
		m.Type = hermes.TypeVideo
		m.Attachments = append(m.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentVideo,
			ID:       u.Message.Video.FileID,
			MimeType: u.Message.Video.MimeType,
		})
	} else if u.Message.Location != nil {
		m.Type = hermes.TypeLocation
		m.Text = fmt.Sprintf("%f,%f", u.Message.Location.Latitude, u.Message.Location.Longitude)
	}

	if u.Message.Document != nil {
		m.Type = hermes.TypeFile
		m.Attachments = append(m.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentVideo,
			ID:       u.Message.Document.FileID,
			MimeType: u.Message.Document.MimeType,
		})
	}

	// System events handling
	if len(u.Message.NewChatMembers) > 0 {
		m.Type = hermes.TypeEvent
		m.Event = &hermes.SystemEvent{
			Type: hermes.EventUserJoined,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(u.Message.NewChatMembers[0].ID, 10),
				Username: u.Message.NewChatMembers[0].Username,
			},
		}
	} else if u.Message.LeftChatMember != nil {
		m.Type = hermes.TypeEvent
		m.Event = &hermes.SystemEvent{
			Type: hermes.EventUserLeft,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(u.Message.LeftChatMember.ID, 10),
				Username: u.Message.LeftChatMember.Username,
			},
		}
	}

	m.Metadata = map[string]any{
		"raw_update_id": u.UpdateID,
	}

	return m
}

func (p *Poller) SendMessage(ctx context.Context, req hermes.MessageRequest) error {
	endpoint := "sendMessage"
	payload := map[string]any{
		"chat_id": req.RecipientID,
		"text":    req.Text,
	}

	if len(req.Attachments) == 1 {
		att := req.Attachments[0]
		switch att.Type {
		case hermes.AttachmentImage:
			endpoint = "sendPhoto"
			payload["photo"] = att.URL
			payload["caption"] = req.Text
			delete(payload, "text")
		case hermes.AttachmentVideo:
			endpoint = "sendVideo"
			payload["video"] = att.URL
			payload["caption"] = req.Text
			delete(payload, "text")
		case hermes.AttachmentFile:
			endpoint = "sendDocument"
			payload["document"] = att.URL
			payload["caption"] = req.Text
			delete(payload, "text")
		}
	} else if len(req.Attachments) > 1 {
		endpoint = "sendMediaGroup"
		var mediaGroup []map[string]any

		for i, att := range req.Attachments {
			mediaItem := map[string]any{
				"media": att.URL, // Telegram supports URLs or File IDs here.
			}

			switch att.Type {
			case hermes.AttachmentImage:
				mediaItem["type"] = "photo"
			case hermes.AttachmentVideo:
				mediaItem["type"] = "video"
			case hermes.AttachmentAudio:
				mediaItem["type"] = "audio"
			default:
				// Fallback to document if type is unknown to prevent Telegram from rejecting the whole array.
				mediaItem["type"] = "document"
			}

			// In an album, the caption usually goes on the first item.
			if i == 0 && req.Text != "" {
				mediaItem["caption"] = req.Text
				delete(payload, "text")
			}

			mediaGroup = append(mediaGroup, mediaItem)
		}

		payload["media"] = mediaGroup
	}

	if req.ReplyToID != "" {
		payload["reply_to_message_id"] = req.ReplyToID
	}

	return p.postToTelegram(ctx, endpoint, payload)
}

func (p *Poller) postToTelegram(ctx context.Context, method string, payload any) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", p.token, method)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create send request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed reading telegram response body with status code %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("telegram API error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
