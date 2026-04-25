package telegram

import (
	"testing"

	"github.com/gargalloeric/hermes"
)

func TestTelegram_mapUpdateToMessage(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) update
		validation func(t *testing.T, got *hermes.Message)
	}

	platform := "telegram"
	tests := []testCase{
		{
			name: "Nil message returns nil",
			setup: func(t *testing.T) update {
				return update{Message: nil}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got != nil {
					t.Error("expected nil hermes message for nil input update")
				}
			},
		},
		{
			name: "Basic text message mapping",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1001,
						Chat:      &chat{ID: 555},
						From:      &user{ID: 99, Username: "test_user", IsBot: false},
						Text:      "Hello Hermes",
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got == nil {
					t.Fatal("got nil message")
				}
				if got.ID != "1001" || got.Platform != platform || got.ChatID != "555" {
					t.Errorf("metadata mismatch: %+v", got)
				}
				if got.Text != "Hello Hermes" || got.Type != hermes.TypeText {
					t.Errorf("content mismatch: %s (%v)", got.Text, got.Type)
				}
			},
		},
		{
			name: "Caption overrides Text",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Text:    "Original Text",
						Caption: "Override Caption",
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Text != "Override Caption" {
					t.Errorf("expected caption to override text, got: %s", got.Text)
				}
			},
		},
		{
			name: "Multimedia: Photo picks highest resolution",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Photo: []photoSize{
							{FileID: "low_res", Size: 10},
							{FileID: "high_res", Size: 100},
						},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeImage || got.Attachments[0].ID != "high_res" {
					t.Errorf("failed to pick highest resolution: %+v", got.Attachments)
				}
			},
		},
		{
			name: "Multimedia: Video with metadata",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Video: &video{
							FileID:   "v_1",
							FileName: "video.mp4",
							MimeType: "video/mp4",
						},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeVideo || got.Attachments[0].FileName != "video.mp4" {
					t.Errorf("video mapping failed: %+v", got.Attachments)
				}
			},
		},
		{
			name: "Multimedia: Voice mapping",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Voice: &voice{FileID: "audio_1", MimeType: "audio/ogg"},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeAudio || got.Attachments[0].MimeType != "audio/ogg" {
					t.Errorf("voice mapping failed: %+v", got.Attachments)
				}
			},
		},
		{
			name: "Multimedia: Document mapping",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Document: &document{FileID: "doc_1", FileName: "test.pdf", MimeType: "application/pdf"},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeFile || got.Attachments[0].FileName != "test.pdf" {
					t.Errorf("document mapping failed: %+v", got.Attachments)
				}
			},
		},
		{
			name: "Multimedia: Location string formatting",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						Location: &location{Latitude: 10.5, Longitude: 20.5},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				expected := "10.500000,20.500000"
				if got.Type != hermes.TypeLocation || got.Text != expected {
					t.Errorf("expected location %s, got %s", expected, got.Text)
				}
			},
		},
		{
			name: "Event: New Member Joined",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						NewChatMembers: []user{{ID: 7, Username: "lucky"}},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeEvent || got.Event.Type != hermes.EventUserJoined {
					t.Errorf("event joined mapping failed")
				}
			},
		},
		{
			name: "Event: Member Left",
			setup: func(t *testing.T) update {
				return update{
					Message: &message{
						MessageID: 1, Chat: &chat{ID: 1}, From: &user{ID: 1},
						LeftChatMember: &user{ID: 8, Username: "gone"},
					},
				}
			},
			validation: func(t *testing.T, got *hermes.Message) {
				if got.Type != hermes.TypeEvent || got.Event.Type != hermes.EventUserLeft {
					t.Errorf("event left mapping failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.setup(t)
			got := mapUpdateToMessage(platform, input)
			tt.validation(t, got)
		})
	}
}

func TestTelegram_mapAction(t *testing.T) {
	type testCase struct {
		name       string
		setup      func(t *testing.T) hermes.ActionType
		validation func(t *testing.T, got string)
	}

	tests := []testCase{
		{
			name: "Typing action",
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
			name: "Voice recording action",
			setup: func(t *testing.T) hermes.ActionType {
				return hermes.ActionRecordVoice
			},
			validation: func(t *testing.T, got string) {
				if got != "record_voice" {
					t.Errorf("expected record_voice, got %s", got)
				}
			},
		},
		{
			name: "Default case for unmapped actions",
			setup: func(t *testing.T) hermes.ActionType {
				return hermes.ActionType("unknown_action")
			},
			validation: func(t *testing.T, got string) {
				if got != "typing" {
					t.Errorf("expected default typing, got %s", got)
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
