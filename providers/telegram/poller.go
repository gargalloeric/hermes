package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gargalloeric/hermes"
)

type Poller struct {
	token      string
	client     *http.Client
	offset     int
	baseURL    string
	backoff    time.Duration
	maxRetries int
}

func NewPoller(token string) *Poller {
	return &Poller{
		token: token,
		client: &http.Client{
			Timeout: 70 * time.Second, // Must be longer than the Telegram timeout
		},
		baseURL:    "https://api.telegram.org/bot%s/%s",
		backoff:    1 * time.Second,
		maxRetries: 2,
	}
}

func (p *Poller) Name() string {
	return "telegram"
}

func (p *Poller) Listen(ctx context.Context, out chan<- *hermes.Message) error {
	timer := time.NewTimer(p.backoff)
	defer timer.Stop()

	failCount := 0

	for {
		updates, err := p.getUpdates(ctx)

		var nextWait time.Duration

		if err != nil {
			// On error calculate the backoff and wait.
			failCount++
			nextWait = p.calculateBackoff(err, failCount)
		} else {
			failCount = 0
			for _, upd := range updates {
				if msg := p.mapToHermes(upd); msg != nil {
					out <- msg
				}
				p.offset = upd.UpdateID + 1
			}

			// If we got messages, poll again immediately (0 delay).
			// If no messages, use the standard backoff.
			if len(updates) == 0 {
				nextWait = p.backoff
			}
		}

		timer.Reset(nextWait)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			// Timer fired, continue to next iteration.
			continue
		}
	}
}

func (p *Poller) SendMessage(ctx context.Context, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	endpoint, payload := p.buildPayload(req)

	for range p.maxRetries {
		tgResp, err := p.postToTelegram(ctx, endpoint, payload)
		if err != nil {
			tgError, ok := errors.AsType[*telegramError](err)
			if ok && tgError.RetryAfter > 0 {
				timer := time.NewTimer(tgError.RetryAfter)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
					continue
				}
			}
			return nil, err
		}

		return &hermes.SentMessage{
			ID:       strconv.Itoa(tgResp.Result.MessageID),
			Platform: p.Name(),
			ChatID:   req.RecipientID,
		}, nil
	}

	return nil, fmt.Errorf("failed to send message after %d retries", p.maxRetries)
}

// Note: Telegram only allows editing messages sent by the bot within the last 48 hours.
func (p *Poller) EditMessage(ctx context.Context, target *hermes.SentMessage, req hermes.MessageRequest) (*hermes.SentMessage, error) {
	payload := map[string]any{
		"chat_id":    target.ChatID,
		"message_id": target.ID,
		"text":       req.Text,
	}

	for range p.maxRetries {
		tgResp, err := p.postToTelegram(ctx, "editMessageText", payload)
		if err != nil {
			tgError, ok := errors.AsType[*telegramError](err)
			if ok && tgError.RetryAfter > 0 {
				timer := time.NewTimer(tgError.RetryAfter)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
					continue
				}
			}
			return nil, err
		}

		return &hermes.SentMessage{
			ID:       strconv.Itoa(tgResp.Result.MessageID),
			Platform: p.Name(),
			ChatID:   target.ChatID,
		}, nil
	}

	return nil, fmt.Errorf("failed to edit the message after %d retries", p.maxRetries)
}

func (p *Poller) ActionTimeout() time.Duration {
	return 5 * time.Second
}

func (p *Poller) SendAction(ctx context.Context, req hermes.ActionRequest) error {
	payload := map[string]any{
		"chat_id": req.RecipientID,
		"action":  mapAction(req.Action),
	}

	_, err := p.postToTelegram(ctx, "sendChatAction", payload)
	return err
}

func mapAction(action hermes.ActionType) string {
	switch action {
	case hermes.ActionTyping:
		return "typing"
	default:
		return "typing" // Fallback for non supported actions
	}
}

func (p *Poller) calculateBackoff(err error, fails int) time.Duration {
	tgError, ok := errors.AsType[*telegramError](err)
	// Telegram explicitly tells us to wait (e.g. 429 Too Many Requests)
	if ok && tgError.RetryAfter > 0 {
		return tgError.RetryAfter
	}

	// Exponential backoff for other errors: 2s, 4s, 8s... capped at 1m
	exp := math.Pow(2, float64(fails))
	d := time.Duration(exp) * time.Second
	if d > time.Minute {
		return time.Minute
	}
	return d
}

func (p *Poller) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	params := url.Values{}
	params.Set("offset", strconv.Itoa(p.offset))
	params.Set("timeout", "60")
	params.Set("allowed_updates", `["message","edited_message","chat_member"]`)

	endpoint := fmt.Sprintf(p.baseURL, p.token, "getUpdates")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create polling request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tgResp tgResponse
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return nil, err
	}

	if !tgResp.Ok {
		retry := 0
		if tgResp.Parameters != nil {
			retry = tgResp.Parameters.RetryAfter
		}
		return nil, &telegramError{
			Code:       tgResp.ErrorCode,
			Message:    tgResp.Description,
			RetryAfter: time.Duration(retry) * time.Second,
		}
	}

	return tgResp.Result, nil
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

	p.mapMultimedia(u.Message, m)
	p.mapSystemEvents(u.Message, m)

	m.Metadata = map[string]any{
		"raw_update_id": u.UpdateID,
	}

	return m
}

func (p *Poller) mapMultimedia(tm *tgMessage, hm *hermes.Message) {
	if len(tm.Photo) > 0 {
		hm.Type = hermes.TypeImage

		// Telegram sends multiple sizes; the last one is always the highest resolution.
		largest := tm.Photo[len(tm.Photo)-1]

		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type: hermes.AttachmentImage,
			ID:   largest.FileID,
		})
	} else if tm.Video != nil {
		hm.Type = hermes.TypeVideo
		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentVideo,
			ID:       tm.Video.FileID,
			MimeType: tm.Video.MimeType,
		})
	} else if tm.Document != nil {
		hm.Type = hermes.TypeFile
		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentFile,
			ID:       tm.Document.FileID,
			MimeType: tm.Document.MimeType,
		})
	} else if tm.Location != nil {
		hm.Type = hermes.TypeLocation
		hm.Text = fmt.Sprintf("%f,%f", tm.Location.Latitude, tm.Location.Longitude)
	}
}

func (p *Poller) mapSystemEvents(tm *tgMessage, hm *hermes.Message) {
	if len(tm.NewChatMembers) > 0 {
		hm.Type = hermes.TypeEvent
		hm.Event = &hermes.SystemEvent{
			Type: hermes.EventUserJoined,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(tm.NewChatMembers[0].ID, 10),
				Username: tm.NewChatMembers[0].Username,
			},
		}
	} else if tm.LeftChatMember != nil {
		hm.Type = hermes.TypeEvent
		hm.Event = &hermes.SystemEvent{
			Type: hermes.EventUserLeft,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(tm.LeftChatMember.ID, 10),
				Username: tm.LeftChatMember.Username,
			},
		}
	}
}

func (p *Poller) buildPayload(req hermes.MessageRequest) (string, map[string]any) {
	// Handle simple text
	if len(req.Attachments) == 0 {
		return p.handleOnlyText(req)
	}

	// Handle single attachment
	if len(req.Attachments) == 1 {
		return p.handleSingleAttachment(req)
	}

	// Handle Albums
	return p.handleMultiAttachment(req)
}

func (p *Poller) handleOnlyText(req hermes.MessageRequest) (string, map[string]any) {
	payload := map[string]any{
		"chat_id": req.RecipientID,
		"text":    req.Text,
	}

	if req.ReplyToID != "" {
		payload["reply_to_message_id"] = req.ReplyToID
	}

	return "sendMessage", payload
}

func (p *Poller) handleSingleAttachment(req hermes.MessageRequest) (string, map[string]any) {
	payload := map[string]any{
		"chat_id": req.RecipientID,
	}

	if req.Text != "" {
		payload["caption"] = req.Text
	}

	if req.ReplyToID != "" {
		payload["reply_to_message_id"] = req.ReplyToID
	}

	att := req.Attachments[0]

	switch att.Type {
	case hermes.AttachmentImage:
		payload["photo"] = att.URL
		return "sendPhoto", payload
	case hermes.AttachmentVideo:
		payload["video"] = att.URL
		return "sendVideo", payload
	default:
		payload["document"] = att.URL
		return "sendDocument", payload
	}
}

func (p *Poller) handleMultiAttachment(req hermes.MessageRequest) (string, map[string]any) {
	payload := map[string]any{
		"chat_id": req.RecipientID,
		"media":   p.buildMediaGroup(req),
	}

	if req.ReplyToID != "" {
		payload["reply_to_message_id"] = req.ReplyToID
	}

	return "sendMediaGroup", payload
}

func (p *Poller) buildMediaGroup(req hermes.MessageRequest) []map[string]any {
	group := make([]map[string]any, len(req.Attachments))

	for i, att := range req.Attachments {
		item := map[string]any{
			"media": att.URL,
		}

		switch att.Type {
		case hermes.AttachmentImage:
			item["type"] = "photo"
		case hermes.AttachmentVideo:
			item["type"] = "video"
		default:
			item["type"] = "document"
		}

		if i == 0 && req.Text != "" {
			item["caption"] = req.Text
		}

		group[i] = item
	}

	return group
}

func (p *Poller) postToTelegram(ctx context.Context, method string, payload any) (*tgSendResponse, error) {
	url := fmt.Sprintf(p.baseURL, p.token, method)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create send request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute send request: %w", err)
	}
	defer resp.Body.Close()

	var tgResp tgSendResponse
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		return nil, fmt.Errorf("failed to decode telegram response: %w", err)
	}

	if !tgResp.Ok {
		retry := 0
		if tgResp.Parameters != nil {
			retry = tgResp.Parameters.RetryAfter
		}

		return nil, &telegramError{
			Code:       tgResp.ErrorCode,
			Message:    tgResp.Description,
			RetryAfter: time.Duration(retry) * time.Second,
		}
	}

	return &tgResp, nil
}
