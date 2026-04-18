package telegram

import (
	"net/http"
	"time"

	"github.com/gargalloeric/hermes"
)

// sender sends messages to the Telegram API.
type sender struct {
	token      string
	client     *http.Client
	maxRetries int
}

type sendRequest struct {
	endpoint string
	payload  payload
}

func newSender(token string) *sender {
	return &sender{
		token: token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		maxRetries: 2,
	}
}

func buildSendPayload(req hermes.MessageRequest) sendRequest {
	if len(req.Attachments) == 0 {
		return onlyText(req)
	}

	if len(req.Attachments) == 1 {
		return singleAttachment(req)
	}

	return multipleAttachments(req)
}

func onlyText(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
		Text:   req.Text,
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	sendReq := sendRequest{
		endpoint: "sendMessage",
		payload:  payload,
	}

	return sendReq
}

func singleAttachment(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
	}

	if req.Text != "" {
		payload.Caption = req.Text
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	att := req.Attachments[0]
	var endpoint string

	switch att.Type {
	case hermes.AttachmentImage:
		payload.Photo = att.URL
		endpoint = "sendPhoto"
	case hermes.AttachmentVideo:
		payload.Video = att.URL
		endpoint = "sendVideo"
	default:
		payload.Document = att.URL
		endpoint = "sendDocument"
	}

	sendReq := sendRequest{
		endpoint: endpoint,
		payload:  payload,
	}

	return sendReq
}

func multipleAttachments(req hermes.MessageRequest) sendRequest {
	payload := payload{
		ChatID: req.RecipientID,
		Media:  getMediaGroup(req),
	}

	if req.ReplyToID != "" {
		payload.ReplyToMessageID = req.ReplyToID
	}

	sendReq := sendRequest{
		endpoint: "sendMediaGroup",
		payload:  payload,
	}

	return sendReq
}

func getMediaGroup(req hermes.MessageRequest) []payloadMedia {
	group := make([]payloadMedia, len(req.Attachments))

	for i, att := range req.Attachments {
		item := payloadMedia{
			Media: att.URL,
			Type:  getMediaType(att.Type),
		}

		if i == 0 && req.Text != "" {
			item.Caption = req.Text
		}

		group[i] = item
	}

	return group
}

func getMediaType(att hermes.AttachmentType) string {
	switch att {
	case hermes.AttachmentImage:
		return "photo"
	case hermes.AttachmentVideo:
		return "video"
	default:
		return "document"
	}
}
