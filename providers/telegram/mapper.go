package telegram

import (
	"fmt"
	"strconv"

	"github.com/gargalloeric/hermes"
)

func (t *Telegram) mapUpdateToMessage(u update) *hermes.Message {
	if u.Message == nil {
		return nil
	}

	m := &hermes.Message{
		ID:       strconv.Itoa(u.Message.MessageID),
		Platform: t.Name(),
		ChatID:   strconv.FormatInt(u.Message.Chat.ID, 10),
		Sender: hermes.User{
			ID:       strconv.FormatInt(u.Message.From.ID, 10),
			Username: u.Message.From.Username,
			IsBot:    u.Message.From.IsBot,
		},
		Text: u.Message.Text,
		Type: hermes.TypeText,
	}

	if u.Message.Caption != "" {
		m.Text = u.Message.Caption
	}

	if isEvent := mapEvent(u.Message, m); !isEvent {
		mapMultimedia(u.Message, m)
	}

	return m
}

func mapMultimedia(um *message, hm *hermes.Message) {
	if len(um.Photo) > 0 {
		hm.Type = hermes.TypeImage

		// Telegram sends multiple sizes, the last one is always the highest resolution.
		largest := um.Photo[len(um.Photo)-1]

		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type: hermes.AttachmentImage,
			ID:   largest.FileID,
		})
	} else if um.Video != nil {
		hm.Type = hermes.TypeVideo
		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentVideo,
			ID:       um.Video.FileID,
			MimeType: um.Video.MimeType,
			FileName: um.Video.FileName,
		})
	} else if um.Voice != nil {
		hm.Type = hermes.TypeAudio
		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentAudio,
			ID:       um.Voice.FileID,
			MimeType: um.Voice.MimeType,
		})
	} else if um.Document != nil {
		hm.Type = hermes.TypeFile
		hm.Attachments = append(hm.Attachments, hermes.Attachment{
			Type:     hermes.AttachmentFile,
			ID:       um.Document.FileID,
			FileName: um.Document.FileName,
			MimeType: um.Document.MimeType,
		})
	} else if um.Location != nil {
		hm.Type = hermes.TypeLocation
		hm.Text = fmt.Sprintf("%f,%f", um.Location.Latitude, um.Location.Longitude)
	}
}

func mapEvent(um *message, hm *hermes.Message) bool {
	if len(um.NewChatMembers) > 0 {
		hm.Type = hermes.TypeEvent
		hm.Event = &hermes.SystemEvent{
			Type: hermes.EventUserJoined,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(um.NewChatMembers[0].ID, 10),
				Username: um.NewChatMembers[0].Username,
				IsBot:    um.NewChatMembers[0].IsBot,
			},
		}
		return true
	} else if um.LeftChatMember != nil {
		hm.Type = hermes.TypeEvent
		hm.Event = &hermes.SystemEvent{
			Type: hermes.EventUserLeft,
			TargetUser: &hermes.User{
				ID:       strconv.FormatInt(um.LeftChatMember.ID, 10),
				Username: um.LeftChatMember.Username,
				IsBot:    um.LeftChatMember.IsBot,
			},
		}
		return true
	}

	return false
}
