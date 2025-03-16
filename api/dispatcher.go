package api

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Bot defines the interface that must be implemented by a struct representing
// an active Telegram session for a specific chat (e.g., a user or group).
// The Update method is called by the Dispatcher whenever a new update
// (e.g., message, callback query) is received from Telegram for that chat.

type Bot interface {
	Update(*Update)
}

// NewBotFn is called every time Neotron receives an update from a chat ID never encountered before.
type NewBotFn func(chatId int64) Bot

// Dispatcher manages Telegram bot updates with semaphore-based concurrency.
type Dispatcher struct {
	api          *API
	newBot       NewBotFn
	updateChan   chan *Update
	rateLimiters sync.Map
	sessions     sync.Map
	wg           sync.WaitGroup
	isRunning    atomic.Bool
	inShutdown   atomic.Bool

	// Optional fields with defaults set by NewDispatcher
	logger           *slog.Logger
	rateLimit        rate.Limit
	rateLimitBurst   int
	updateBufferSize int
	allowedUpdates   []UpdateType
	workerSem        *semaphore.Weighted
}

type OptionDSP func(*Dispatcher) error

// NewDispatcher creates a new Dispatcher with default values
func NewDispatcher(token string, newBot NewBotFn, opts ...OptionDSP) (*Dispatcher, error) {
	if token == "" {
		return nil, fmt.Errorf("token must be provided")
	}

	if newBot == nil {
		return nil, fmt.Errorf("newBot function must be provided")
	}

	dsp := &Dispatcher{
		newBot:           newBot,
		updateBufferSize: 100,
		rateLimit:        rate.Every(5 * time.Second),
		rateLimitBurst:   3,
		rateLimiters:     sync.Map{},
		sessions:         sync.Map{},
		workerSem:        semaphore.NewWeighted(int64(10)),
		logger:           slog.New(slog.NewTextHandler(os.Stdout, nil)),
		allowedUpdates: []UpdateType{
			MessageUpdate,
			EditedMessageUpdate,
			ChannelPostUpdate,
			EditedChannelPostUpdate,
			InlineQueryUpdate,
			ChosenInlineResultUpdate,
			CallbackQueryUpdate,
			ShippingQueryUpdate,
			PreCheckoutQueryUpdate,
			PollUpdate,
			PollAnswerUpdate,
			MyChatMemberUpdate,
			ChatMemberUpdate,
		},
	}

	for _, opt := range opts {
		if err := opt(dsp); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	dsp.updateChan = make(chan *Update, dsp.updateBufferSize)

	var err error
	dsp.api, err = NewAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create API with token %q: %w", token, err)
	}

	return dsp, nil
}

func WithRateLimit(limit rate.Limit, burst int) OptionDSP {
	return func(d *Dispatcher) error {
		if burst <= 0 {
			return fmt.Errorf("burst must be positive")
		}
		d.rateLimit = limit
		d.rateLimitBurst = burst
		return nil
	}
}

func WithUpdateBufferSize(size int) OptionDSP {
	return func(d *Dispatcher) error {
		if size <= 0 {
			return fmt.Errorf("buffer size must be positive")
		}
		d.updateBufferSize = size
		return nil
	}
}

func WithAllowedUpdates(types []UpdateType) OptionDSP {
	return func(d *Dispatcher) error {
		if types == nil || len(types) == 0 {
			return fmt.Errorf("allowed updates cannot be nil or empty")
		}
		d.allowedUpdates = types
		return nil
	}
}

func WithLogger(logger *slog.Logger) OptionDSP {
	return func(d *Dispatcher) error {
		if logger == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		d.logger = logger
		return nil
	}
}

func WithMaxConcurrentUpdates(threads int64) OptionDSP {
	return func(d *Dispatcher) error {
		if threads <= 0 {
			return fmt.Errorf("max concurrent updates must be positive")
		}
		d.workerSem = semaphore.NewWeighted(threads)
		return nil
	}
}

// Poll begins polling and processing updates
func (d *Dispatcher) Poll(ctx context.Context) error {
	if d.inShutdown.Load() {
		return fmt.Errorf("dispatcher is shutting down")
	}
	if !d.isRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("dispatcher is already running")
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return d.pollUpdates(ctx)
	})

	g.Go(func() error {
		return d.worker(ctx)
	})

	err := g.Wait()
	d.wg.Wait()
	d.isRunning.Store(false)
	d.inShutdown.Store(false)
	return err
}

// worker processes updates from the update channel.
func (d *Dispatcher) worker(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update, ok := <-d.updateChan:
			if !ok {
				return nil
			}

			if err := d.handleUpdate(ctx, update); err != nil {
				d.logger.Warn("failed to handle update", "update_id", update.ID, "chat_id", update.ChatID(), "error", err)
				return err
			}
		}
	}
}

// handleUpdate processes a single update
func (d *Dispatcher) handleUpdate(ctx context.Context, update *Update) error {
	if update == nil {
		return fmt.Errorf("got nil update")
	}

	chatID := update.ChatID()

	limiter, err := d.getLimiter(chatID)
	if err != nil {
		return fmt.Errorf("failed to get rate limiter: %w", err)
	}

	if !limiter.Allow() {
		d.logger.Warn("Dropping", "update_id", update.ID, "chat_id", chatID)
		return nil
	}

	bot, botErr := d.getSession(chatID)
	if botErr != nil {
		return fmt.Errorf("failed to get bot session: %w", err)
	}

	// Acquire semaphore
	if semErr := d.workerSem.Acquire(ctx, 1); semErr != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", semErr)
	}

	d.wg.Add(1)
	go func() {
		defer d.workerSem.Release(1)
		defer d.wg.Done()
		d.logger.Info("Processing", "update_id", update.ID, "chat_id", chatID)
		bot.Update(update)
	}()
	return nil
}

// pollUpdates fetches updates from the API
func (d *Dispatcher) pollUpdates(ctx context.Context) error {
	// Delete the webhook to enable long polling mode
	if _, err := d.api.DeleteWebhook(true); err != nil {
		return fmt.Errorf("deleting webhook: %w", err)
	}

	var opts UpdateOptions
	opts.Timeout = 30
	opts.Limit = 100
	opts.AllowedUpdates = d.allowedUpdates

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			response, err := d.api.GetUpdates(&opts)
			if err != nil {
				return err
			}

			if len(response.Result) == 0 {
				continue
			}

			for _, u := range response.Result {
				d.updateChan <- u
			}

			if l := len(response.Result); l > 0 {
				opts.Offset = response.Result[l-1].ID + 1
			}
		}
	}
}

// getLimiter retrieves or creates a rate limiter for a chat
func (d *Dispatcher) getLimiter(chatID int64) (*rate.Limiter, error) {
	value, loaded := d.rateLimiters.LoadOrStore(chatID, rate.NewLimiter(d.rateLimit, d.rateLimitBurst))
	limiter, ok := value.(*rate.Limiter)
	if !ok {
		return nil, fmt.Errorf("rate limiter for chat %d is of invalid type", chatID)
	}
	if !loaded {
		d.logger.Info("created new rate limiter", "chat_id", chatID)
	}
	return limiter, nil
}

// getSession retrieves or creates a bot session for a chat
func (d *Dispatcher) getSession(chatID int64) (Bot, error) {
	value, loaded := d.sessions.LoadOrStore(chatID, d.newBot(chatID))
	bot, ok := value.(Bot)
	if !ok {
		return nil, fmt.Errorf("bot session for chat %d is of invalid type", chatID)
	}
	if !loaded {
		d.logger.Info("created new bot session", "chat_id", chatID)
	}
	return bot, nil
}
