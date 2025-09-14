package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/acquisitionist/teletron"
	"golang.org/x/time/rate"
)

const (
	defaultShutdownPeriod = 30 * time.Second
)

func (app *application) serveDSP() error {
	dsp := &teletron.Dispatcher{
		Bot:        app.bot,
		NewUpdater: app.newUpdater,
		Limiter:    rate.NewLimiter(rate.Limit(app.config.limiter.rps), app.config.limiter.burst),
		Concurrent: 10,
		ErrorLog:   slog.NewLogLogger(app.logger.Handler(), slog.LevelWarn),
	}

	err := app.serve(dsp)
	if err != nil {
		return err
	}

	app.wg.Wait()
	return nil
}

func (app *application) serve(dsp *teletron.Dispatcher) error {
	shutdownErrorChan := make(chan error)

	go func() {
		quitChan := make(chan os.Signal, 1)
		signal.Notify(quitChan, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(quitChan)

		<-quitChan

		ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownPeriod)
		defer cancel()

		shutdownErrorChan <- dsp.Shutdown(ctx)
	}()

	app.logger.Info("starting dispatcher", slog.Group("dispatcher", "bot", app.bot.User.Username))

	opts := teletron.PollingOpts{
		GetUpdatesOpts: &teletron.GetUpdatesOpts{
			// Specify only the update types your bot cares about
			AllowedUpdates: []string{
				teletron.UpdateTypeMessage,
				teletron.UpdateTypeEditedMessage,
				teletron.UpdateTypeCallbackQuery,
			},
			// Optional: set a timeout for long polling
			Timeout: 30,
			// Optional: set an offset if needed
			Offset: 0,
			Limit:  100,
			RequestOpts: &teletron.RequestOpts{
				Timeout: 35 * time.Second,
				APIURL:  "",
			},
		},
		// Optional: drop pending updates if you don't want old updates
		DropPendingUpdates:    true,
		EnableWebhookDeletion: true,
	}

	err := dsp.Poll(app.bot, &opts)
	if !errors.Is(err, teletron.ErrDispatcherClosed) {
		return err
	}

	if err := <-shutdownErrorChan; err != nil {
		return err
	}

	app.logger.Info("stopped dispatcher", slog.Group("dispatcher", "bot", app.bot.User.Username))
	return nil
}
