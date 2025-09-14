package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/acquisitionist/teletron"
)

// Struct useful for managing internal states in your updater, but it could be of
// any type such as `type updater int64` if you only need to store the chatID.
type updater struct {
	chatID int64
	state  stateFn
	mws    chain
	name   string
	app    *application
}

type stateFn func(*teletron.Update) stateFn

type chain []func(stateFn) stateFn

func (c chain) then(h stateFn) stateFn {
	for _, mw := range slices.Backward(c) {
		h = mw(h)
	}
	return h
}

func loggingMiddleware(b *updater) func(stateFn) stateFn {
	return func(next stateFn) stateFn {
		return func(update *teletron.Update) stateFn {
			typ := update.GetType()
			b.app.logger.Info(
				"Update",
				"update.id", update.UpdateId,
				"chat.id", b.chatID,
				"type", typ,
			)
			return next(update)
		}
	}
}

// Update method to handle incoming updates
func (b *updater) Update(update *teletron.Update) {
	handler := b.mws.then(b.state)
	b.state = handler(update)
}

func (app *application) newUpdater(chatID int64) teletron.Updater {
	b := &updater{
		chatID: chatID,
		app:    app,
	}
	b.state = b.handleUpdateTypes
	b.mws = chain{
		loggingMiddleware(b),
	}
	return b
}

func (b *updater) handleMessage(update *teletron.Update) stateFn {
	if strings.HasPrefix(update.Message.Text, "/set_name") {
		b.app.bot.SendMessage(b.chatID, "send my name", nil)
		return b.handleName
	}
	return b.handleUpdateTypes
}

func (b *updater) handleName(update *teletron.Update) stateFn {
	b.name = update.Message.Text
	b.app.bot.SendMessage(b.chatID, (fmt.Sprintf("My new name is %q", b.name)), nil)
	// Here we return b.handleMessage since the next time we receive a message
	// it will be handled in the default way.
	return b.handleMessage
}

func (b *updater) handleUpdateTypes(update *teletron.Update) stateFn {
	if update == nil {
		b.app.logger.Warn(
			"Nil Update",
			"update.id", update.UpdateId,
			"chat.id", b.chatID,
		)
		return b.handleUpdateTypes
	}

	switch update.GetType() {
	case teletron.UpdateTypeMessage:
		return b.handleMessage(update)
	case teletron.UpdateTypeMyChatMember:
		return b.handleUpdateTypes
	default:
		b.app.logger.Warn(
			"Unhandled Update",
			"update.id", update.UpdateId,
			"chat.id", b.chatID,
			"type", update.GetType(),
		)
		return b.handleUpdateTypes
	}
}
