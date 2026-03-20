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

	// TODO: add NewChatMembers here later for System Events
}

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgPhotoSize struct {
	FileID string `json:"file_id"`
}
