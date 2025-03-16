package main

import (
	"context"
	"errors"
	"github.com/acquisitionist/teletron/api"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// Struct useful for managing internal states in your bot, but it could be of
// any type such as `type bot int64` if you only need to store the chatID.
type bot struct {
	chatID int64
	api    *api.API
}

// Update method is needed to implement the neotron.Bot interface.
func (b *bot) Update(update *api.Update) {
	if update.Message.Text == "/start" {
		_, err := b.api.SendMessage("Hello world", b.chatID, nil)
		if err != nil {
			return
		}
	}
}

func newBot(chatID int64) api.Bot {
	myAPI, err := api.NewAPI("TOKEN_HERE")
	if err != nil {
		panic(err)
	}
	return &bot{
		chatID: chatID,
		api:    myAPI,
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dsp, err := api.NewDispatcher("TOKEN_HERE", newBot, api.WithLogger(logger))
	if err != nil {
		slog.Error("Failed to create dispatcher", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		slog.Info("Received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Start the dispatcher and block until it stops
	if pollErr := dsp.Poll(ctx); pollErr != nil && !errors.Is(pollErr, context.Canceled) {
		slog.Error("Dispatcher failed", "error", pollErr)
		os.Exit(1)
	}

	slog.Info("Program exited successfully")
}
