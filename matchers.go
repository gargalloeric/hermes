package hermes

import (
	"strings"
)

// OnText matches any message of type Text.
func (c *Client) OnText(h Handler) {
	c.On(func(m *Message) bool { return m.Type == TypeText }, h)
}

// OnCommand matches text starting with a specific command (e.g., "/start").
func (c *Client) OnCommand(cmd string, h Handler) {
	c.On(func(m *Message) bool { return strings.HasPrefix(m.Text, cmd) }, h)
}

// OnImage matches a message of type Image.
func (c *Client) OnImage(h Handler) {
	c.On(func(m *Message) bool { return m.Type == TypeImage }, h)
}

func (c *Client) OnEvent(event EventType, h Handler) {
	c.On(func(m *Message) bool { return m.Event.Type == event }, h)
}

// And combines multiple matchers; all must return true.
func And(matchers ...Matcher) Matcher {
	return func(m *Message) bool {
		for _, matcher := range matchers {
			if !matcher(m) {
				return false
			}
		}

		return true
	}
}

// Or combines multiple matchers; at least one must return true.
func Or(matchers ...Matcher) Matcher {
	return func(m *Message) bool {
		for _, matcher := range matchers {
			if matcher(m) {
				return true
			}
		}

		return false
	}
}

func IsImage(m *Message) bool {
	return m.Type == TypeImage
}
