package telegram

import (
	"testing"

	"github.com/gargalloeric/hermes"
)

func TestMapToHermes(t *testing.T) {
	p := NewPoller("fake-token")

	tests := []struct {
		name     string
		input    tgUpdate
		validate func(*testing.T, *hermes.Message)
	}{
		{
			name: "Simple Text Message",
			input: tgUpdate{
				Message: &tgMessage{
					MessageID: 1,
					From:      tgUser{ID: 123, Username: "hermes"},
					Text:      "Hello",
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Text != "Hello" || m.Type != hermes.TypeText {
					t.Errorf("expected text 'Hello', got %s", m.Text)
				}
			},
		},
		{
			name: "Photo with Caption (Highest Res)",
			input: tgUpdate{
				Message: &tgMessage{
					Caption: "Look at this!",
					Photo: []tgPhotoSize{
						{FileID: "low-res"},
						{FileID: "high-res"},
					},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeImage || m.Text != "Look at this!" {
					t.Error("failed to map image or caption")
				}
				if m.Attachments[0].ID != "high-res" {
					t.Errorf("expected highest resolution 'high-res', got %s", m.Attachments[0].ID)
				}
			},
		},
		{
			name: "User Joined Event",
			input: tgUpdate{
				Message: &tgMessage{
					NewChatMembers: []tgUser{{ID: 456, Username: "Hermes"}},
				},
			},
			validate: func(t *testing.T, m *hermes.Message) {
				if m.Type != hermes.TypeEvent || m.Event.Type != hermes.EventUserJoined {
					t.Error("failed to map join event")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := p.mapToHermes(tc.input)
			if msg == nil {
				t.Fatal("expected message, got nil!")
			}

			tc.validate(t, msg)
		})
	}
}
