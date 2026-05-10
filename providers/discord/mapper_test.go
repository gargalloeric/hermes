package discord

import (
	"testing"

	"github.com/gargalloeric/hermes"
)

func TestDiscord_mapMessageToHermes(t *testing.T) {
	type testCase struct {
		name       string
		platform   string
		setup      func(t *testing.T) message
		validation func(t *testing.T, got *hermes.Message)
	}

	tests := []testCase{
		{
			name:     "Basic text message mapping",
			platform: "discord",
			setup: func(t *testing.T) message {
				return message{
					ID:        "msg_123",
					Content:   "Hello from Discord",
					ChannelID: "chan_456",
					Author: user{
						ID:       "user_789",
						Username: "Gopher",
						Bot:      false,
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.ID != "msg_123" || got.Text != "Hello from Discord" {
					t.Errorf("content mismatch: ID=%s, Text=%s", got.ID, got.Text)
				}
				if got.Type != hermes.TypeText {
					t.Errorf("expected TypeText, got %v", got.Type)
				}
				if got.Sender.ID != "user_789" || got.Sender.Username != "Gopher" {
					t.Errorf("sender mismatch: %+v", got.Sender)
				}
			},
		},
		{
			name: "Type escalation to Image",
			setup: func(t *testing.T) message {
				return message{
					Attachments: []attachment{
						{
							ID:          "att_img",
							ContentType: "image/jpeg",
							Filename:    "photo.jpg",
						},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				// The logic says: if att.Type != AttachmentFile, m.Type = MessageType(att.Type)
				if got.Type != hermes.TypeImage {
					t.Errorf("expected TypeImage, got %v", got.Type)
				}
				if got.Attachments[0].Type != hermes.AttachmentImage {
					t.Errorf("attachment type mismatch: %v", got.Attachments[0].Type)
				}
			},
		},
		{
			name: "Type remains Text for generic File attachment",
			setup: func(t *testing.T) message {
				return message{
					Attachments: []attachment{
						{
							ID:          "att_file",
							ContentType: "application/pdf",
							Filename:    "resume.pdf",
						},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				// Per logic: if att.Type == AttachmentFile, it doesn't overwrite m.Type
				if got.Type != hermes.TypeText {
					t.Errorf("expected type to remain TypeText for files, got %v", got.Type)
				}
				if got.Attachments[0].Type != hermes.AttachmentFile {
					t.Errorf("attachment type should be AttachmentFile, got %v", got.Attachments[0].Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.setup(t)
			got := mapMessageToHermes(tt.platform, msg)
			tt.validation(t, got)
		})
	}
}

func TestDiscord_mapAttachmentType(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) (string, string)
		validation func(t *testing.T, got hermes.AttachmentType)
	}

	tests := []testCase{
		{
			name: "Image via Mime",
			setup: func(t *testing.T) (string, string) {
				return "image/png", ""
			},
			validation: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentImage {
					t.Errorf("expected AttachmentImage, got %s", got)
				}
			},
		},
		{
			name: "Video via Extension",
			setup: func(t *testing.T) (string, string) {
				return "", "clip.MP4"
			},
			validation: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentVideo {
					t.Errorf("expected AttachmentVideo, got %s", got)
				}
			},
		},
		{
			name: "Audio via Extension",
			setup: func(t *testing.T) (string, string) {
				return "", "voice.ogg"
			},
			validation: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentAudio {
					t.Errorf("expected AttachmentAudio, got %s", got)
				}
			},
		},
		{
			name: "Fallback to File",
			setup: func(t *testing.T) (string, string) {
				return "application/zip", "data.zip"
			},
			validation: func(t *testing.T, got hermes.AttachmentType) {
				if got != hermes.AttachmentFile {
					t.Errorf("expected AttachmentFile, got %s", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mime, file := tt.setup(t)
			got := mapAttachmentType(mime, file)
			tt.validation(t, got)
		})
	}
}

func TestDiscord_mapMedia(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) []hermes.Attachment
		validation func(t *testing.T, embeds []embed, files []hermes.Attachment)
	}

	tests := []testCase{
		{
			name: "Correct segregation of visual and data attachments",
			setup: func(t *testing.T) []hermes.Attachment {
				return []hermes.Attachment{
					{Type: hermes.AttachmentImage, URL: "url1", FileName: "img.png"},
					{Type: hermes.AttachmentVideo, URL: "url2", FileName: "vid.mp4"},
					{Type: hermes.AttachmentFile, URL: "url3", FileName: "doc.pdf"},
					{Type: hermes.AttachmentAudio, URL: "url4", FileName: "sound.mp3"},
				}
			},
			validation: func(t *testing.T, embeds []embed, files []hermes.Attachment) {
				if len(embeds) != 2 {
					t.Errorf("expected 2 embeds (img/vid), got %d", len(embeds))
				}
				// Audio and File should both end up in the files slice
				if len(files) != 2 {
					t.Errorf("expected 2 files (doc/sound), got %d", len(files))
				}
				if embeds[0].Image == nil || embeds[0].Image.URL != "url1" {
					t.Error("image embed mapping failed")
				}
				if embeds[1].Video == nil || embeds[1].Video.URL != "url2" {
					t.Error("video embed mapping failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.setup(t)
			embeds, rawFiles := mapMedia(input)
			tt.validation(t, embeds, rawFiles)
		})
	}
}

func TestDiscord_mapAction(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) hermes.ActionType
		validation func(t *testing.T, got string)
	}

	tests := []testCase{
		{
			name: "Typing mapping",
			setup: func(t *testing.T) hermes.ActionType {
				return hermes.ActionTyping
			},
			validation: func(t *testing.T, got string) {
				if got != "typing" {
					t.Errorf("expected typing, got %s", got)
				}
			},
		},
		{
			name: "Unsupported action fallback",
			setup: func(t *testing.T) hermes.ActionType {
				return hermes.ActionRecordVoice
			},
			validation: func(t *testing.T, got string) {
				// Discord logic specifically falls back to typing
				if got != "typing" {
					t.Errorf("expected record_voice to fallback to typing, got %s", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := tt.setup(t)
			got := mapAction(action)
			tt.validation(t, got)
		})
	}
}
