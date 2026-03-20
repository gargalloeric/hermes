package main

import (
	"context"
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
		log.Fatalln("TELEGRAM_TOKEN environment variable is required")
	}

	tgProvider := telegram.NewPoller(token)

	chat := hermes.New(
		hermes.WithProvider(tgProvider),
	)

	chat.OnCommand("/start", func(c *hermes.Context) {
		c.Send("Hermes is alive 🛡️!")
	})

	chat.OnCommand("/ping", func(c *hermes.Context) {
		c.Send("Pong!", hermes.AsReply())
	})

	chat.OnText(func(c *hermes.Context) {
		c.Send("You said: " + c.Message.Text)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Println("Starting Hermes bot... Press Ctr+C to stop.")

	if err := chat.Start(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Hermes stopped with error: %v", err)
	}

	log.Println("Hermes shut down cleanly.")
}
