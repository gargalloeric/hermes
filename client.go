package hermes

import (
	"context"
	"log"
)

// Handler is the function signature for chat logic.
type Handler func(c *Context)

// Matcher is a function that decides if a handler should run for a given message.
type Matcher func(m *Message) bool

type route struct {
	matcher Matcher
	handler Handler
}

type Client struct {
	providers []Provider
	routes    []route
}

// New initializes the client with functional options.
func New(opts ...ClientOption) *Client {
	c := &Client{}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Client) On(m Matcher, h Handler) {
	c.routes = append(c.routes, route{
		matcher: m,
		handler: h,
	})
}

func (c *Client) Start(ctx context.Context) error {
	// A buffered channel to prevent slow dispatching from blocking providers
	messageChan := make(chan *Message, 100)

	for _, p := range c.providers {
		go func(prov Provider) {
			if err := prov.Listen(ctx, messageChan); err != nil {
				log.Printf("Provider %s exited with error: %v", prov.Name(), err)
			}
		}(p)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-messageChan:
			c.dispatch(ctx, msg)
		}
	}
}

func (c *Client) dispatch(ctx context.Context, msg *Message) {
	var source Provider
	for _, p := range c.providers {
		if p.Name() == msg.Platform {
			source = p
			break
		}
	}

	if source == nil {
		log.Printf("Warning: Received message for unknown platform %s", msg.Platform)
		return
	}

	for _, r := range c.routes {
		if r.matcher(msg) {
			hermesCtx := NewContext(ctx, source, msg)
			go r.handler(hermesCtx)

			return
		}
	}
}
