package hermes

import "strings"

// OnText matches any message of type Text.
func (c *Client) OnText(h Handler) {
	c.On(func(m *Message) bool { return m.Type == TypeText }, h)
}

// OnCommand matches text starting with a specific command (e.g., "/start").
func (c *Client) OnCommand(cmd string, h Handler) {
	c.On(func(m *Message) bool { return strings.HasPrefix(m.Text, cmd) }, h)
}
