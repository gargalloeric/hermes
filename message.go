package hermes

type MessageType string
type AttachmentType string
type EventType string

const (
	TypeText     MessageType = "text"
	TypeImage    MessageType = "image"
	TypeVideo    MessageType = "video"
	TypeAudio    MessageType = "audio"
	TypeLocation MessageType = "location"
	TypeEvent    MessageType = "event" // For system notifications
)

const (
	AttachmentImage AttachmentType = "image"
	AttachmentVideo AttachmentType = "video"
	AttachmentAudio AttachmentType = "audio"
	AttachmentFile  AttachmentType = "file"
)

const (
	EventUserJoined  EventType = "user_joined"
	EventUserLeft    EventType = "user_left"
	EventMessageEdit EventType = "message_edited"
)

type User struct {
	ID       string
	Username string
}

type Attachment struct {
	Type     AttachmentType
	URL      string
	ID       string // Platform-specific file reference
	MimeType string
}

type SystemEvent struct {
	Type       EventType
	TargetUser *User // The user involved in the event
}

type Message struct {
	ID          string
	Platform    string // e.g. telegram, discord, etc...
	Sender      User
	Text        string
	Type        MessageType
	Attachments []Attachment
	Event       *SystemEvent
	Metadata    map[string]any // Escape hatch for platform-specific raw data
}
