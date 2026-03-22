package telegram

// tgResponse is the envelope for all Telegram API responses
type tgResponse struct {
	Ok     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

type tgUpdate struct {
	UpdateID int        `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	MessageID int           `json:"message_id"`
	From      tgUser        `json:"from"`
	Text      string        `json:"text"`
	Caption   string        `json:"caption"`
	Photo     []tgPhotoSize `json:"photo"`
	Video     *tgVideo      `json:"video"`
	Document  *tgDocument   `json:"document"`
	Voice     *tgVoice      `json:"voice"`
	Location  *tgLocation   `json:"location"`

	// System Events
	NewChatMembers []tgUser `json:"new_chat_members"`
	LeftChatMember *tgUser  `json:"left_chat_member"`
}

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgPhotoSize struct {
	FileID string `json:"file_id"`
}

type tgVideo struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
}

type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
}

type tgVoice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
}

type tgLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type tgSendResponse struct {
	Ok     bool      `json:"ok"`
	Result tgMessage `json:"result"`
}
