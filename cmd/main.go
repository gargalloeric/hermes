package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gargalloeric/hermes"
	"github.com/gargalloeric/hermes/providers/telegram"
)

func main() {
	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("Set TELEGRAM_TOKEN environment variable")
	}

	// Initialize a provider, e.g. Telegram
	tg := telegram.NewPoller(token)

	// Initialize the client with the providers.
	client := hermes.New(hermes.WithProvider(tg))

	client.OnCommand("/ping", func(c *hermes.Context) {
		// Standard send
		c.Send("Pong! 🏓")
	})

	client.OnCommand("/quote", func(c *hermes.Context) {
		// Send as a formal reply/quote
		c.Send("This message formally quotes your command.", hermes.AsReply())
	})

	// React to images messages
	client.OnImage(func(c *hermes.Context) {
		imageID := c.Message.Attachments[0].ID
		c.Send(fmt.Sprintf("I see your image! Internal ID: %s", imageID), hermes.AsReply())
	})

	// Send images to the chat
	client.OnCommand("/img", func(c *hermes.Context) {
		dogURL := "https://w.wallhaven.cc/full/5y/wallhaven-5y5537.png"
		c.Send("Here is an image for you!", hermes.WithImage(dogURL))
	})

	// React to events
	client.OnEvent(hermes.EventUserJoined, func(c *hermes.Context) {
		newUser := c.Message.Event.TargetUser.Username
		c.Send(fmt.Sprintf("Welcome to the interface layer, @%s! 🛡️", newUser))
	})

	// Send documents to the chat
	client.OnCommand("/report", func(c *hermes.Context) {
		reportURL := "https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf"

		c.Send("Here is the requested document.", hermes.WithDocument(reportURL))
	})

	// Customize the routing predicate with a custom Matcher
	client.On(func(m *hermes.Message) bool { return m.Type == hermes.TypeLocation }, func(c *hermes.Context) {
		c.Send("You are currently at coordinates: " + c.Message.Text)
	})

	// React to simple text messges
	client.OnText(func(c *hermes.Context) {
		c.Send("Echoing your text: " + c.Message.Text)
	})

	// Graceful Shutdown Setup
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("Hermes Lab is running... Send a command, an image, or a location to the bot.")
	if err := client.Start(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Fatal error: %v", err)
	}
}
