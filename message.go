package hermes

// MessageType defines the primary category of an incoming or outgoing message.
type MessageType string

const (
	TypeText     MessageType = "text"
	TypeImage    MessageType = "image"
	TypeVideo    MessageType = "video"
	TypeAudio    MessageType = "audio"
	TypeLocation MessageType = "location"
	TypeFile     MessageType = "file"
	TypeEvent    MessageType = "event" // For system notifications
)

// AttachmentType defines the specific nature of a file attached to a message.
type AttachmentType string

const (
	AttachmentImage AttachmentType = "image"
	AttachmentVideo AttachmentType = "video"
	AttachmentAudio AttachmentType = "audio"
	AttachmentFile  AttachmentType = "file"
)

// EventType defines the specific action for a SystemEvent.
type EventType string

const (
	EventUserJoined EventType = "user_joined"
	EventUserLeft   EventType = "user_left"
)

// ActionType defines the specific action to show in the platform UI.
type ActionType string

const (
	ActionTyping      ActionType = "typing"
	ActionRecordVoice ActionType = "record_voice"
)

// User represents a participant in a chat on any platform.
type User struct {
	ID       string
	Username string
	IsBot    bool
}

// Attachment represents a media file or document associated with a message.
type Attachment struct {
	Type     AttachmentType
	URL      string
	FileName string
	ID       string // Platform-specific file reference
	MimeType string
}

// SystemEvent contains details about non-content messages like joins or leaves.
type SystemEvent struct {
	Type       EventType
	TargetUser *User // The user involved in the event
}

// Message represents a universal chat message, abstracted from platform-specific details.
type Message struct {
	ID          string // Unique identifier for the message on the platform.
	Platform    string // The name of the provider (e.g., "telegram", "discord").
	Sender      User   // The user who sent the message.
	ChatID      string
	Text        string         // The text content or caption of the message.
	Type        MessageType    // The category of the message (Text, Image, Event, etc...).
	Attachments []Attachment   // List of files associated with this message
	Event       *SystemEvent   // Details if the message is a SystemEvent
	Metadata    map[string]any // Escape hatch for platform-specific raw data
}

// SentMessage is the "receipt" returned by a provider after a successful send operation.
type SentMessage struct {
	ID       string
	Platform string
	ChatID   string
	Metadata map[string]string
}
