package discord

import (
	"path/filepath"
	"strings"

	"github.com/gargalloeric/hermes"
)

func mapMessageToHermes(platform string, msg message) *hermes.Message {
	m := &hermes.Message{
		ID:       msg.ID,
		Platform: platform,
		Text:     msg.Content,
		ChatID:   msg.ChannelID,
		Sender: hermes.User{
			ID:       msg.Author.ID,
			Username: msg.Author.Username,
			IsBot:    msg.Author.Bot,
		},
		Type: hermes.TypeText,
	}

	if len(msg.Attachments) > 0 {
		m.Attachments = mapAttachments(msg.Attachments)

		for _, att := range m.Attachments {
			if att.Type != hermes.AttachmentFile {
				m.Type = hermes.MessageType(att.Type)
			}
		}
	}

	return m
}

func mapAttachments(atts []attachment) []hermes.Attachment {
	hermesAtts := make([]hermes.Attachment, 0, len(atts))

	for _, att := range atts {
		resolvedType := mapAttachmentType(att.ContentType, att.Filename)

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

func mapAttachmentType(contentType, filename string) hermes.AttachmentType {
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

	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return hermes.AttachmentImage
	case ".mp4", ".mov", ".webm":
		return hermes.AttachmentVideo
	case ".mp3", ".ogg", ".wav":
		return hermes.AttachmentAudio
	default:
		return hermes.AttachmentFile
	}
}

func mapEmbeds(atts []hermes.Attachment) []embed {
	var embeds []embed
	for _, att := range atts {
		switch att.Type {
		case hermes.AttachmentImage:
			embeds = append(embeds, embed{
				Title: att.FileName,
				URL:   att.URL,
				Image: embedMedia{
					URL: att.URL,
				},
			})
		case hermes.AttachmentVideo:
			embeds = append(embeds, embed{
				Title: att.FileName,
				URL:   att.URL,
				Video: embedMedia{
					URL: att.URL,
				},
			})
		}
	}

	return embeds
}
