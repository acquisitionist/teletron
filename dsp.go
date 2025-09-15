package teletron

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

var (
	ErrDispatcherClosed          = errors.New("teletron: Dispatcher closed")
	ErrDispatcherNilUpdaterInput = errors.New("teletron: Nil input provided as NewUpdaterFn")
)

type Updater interface {
	Update(*Update)
}

type NewUpdaterFn func(chatId int64) Updater

type ErrorFunc func(error)
type Dispatcher struct {
	Bot            *Bot
	Limiter        *rate.Limiter
	NewUpdater     NewUpdaterFn
	AllowedUpdates []string
	ErrorLog       *log.Logger
	// Concurrent is used to decide how to limit the number of goroutines spawned by the dispatcher.
	// This defines how many updates can be processed at the same time.
	// If Concurrent == 0, Gomaxproc is used instead.
	// If Concurrent < 0, no limits are imposed.
	// If Concurrent > 0, that value is used.
	Concurrent int
	// UnhandledErrFunc provides more flexibility for dealing with unhandled update processing errors.
	// This includes errors when unmarshalling updates, unhandled panics during handler executions, or unknown
	// dispatcher actions.
	// If nil, the error goes to ErrorLog.
	UnhandledErrFunc ErrorFunc

	current dispatcherState

	mu         sync.Mutex
	inShutdown atomic.Bool // true when server is in shutdown
	onShutdown []func()
	wg         sync.WaitGroup
}

// dispatcherState maintains runtime state for the Dispatcher.
type dispatcherState struct {
	sem        chan struct{}
	cancel     context.CancelFunc
	updateChan chan *Update
	sessions   sync.Map
}

type session struct {
	bot     Updater
	limiter *rate.Limiter
}

// PollingOpts represents the optional values to start long polling.
type PollingOpts struct {
	// DropPendingUpdates toggles whether to drop updates which were sent before the bot was started.
	// This also implicitly enables webhook deletion.
	DropPendingUpdates bool
	// EnableWebhookDeletion deletes any existing webhooks to ensure that the updater works fine.
	EnableWebhookDeletion bool
	// GetUpdatesOpts represents the opts passed to GetUpdates.
	// Note: It is recommended you edit the values here when running in production environments.
	// Suggestions include:
	//    - Changing the "GetUpdatesOpts.AllowedUpdates" to only refer to updates relevant to your bot's functionality.
	//    - Using a non-0 "GetUpdatesOpts.Timeout" value. This is how "long" telegram will hold the long-polling call
	//    while waiting for new messages. A value of 0 causes telegram to reply immediately, which will then cause
	//    your bot to immediately ask for more updates. While this can seem fine, it will eventually causing
	//    telegram to delay your requests when left running over longer periods. If you are seeing lots
	//    of "context deadline exceeded" errors on GetUpdates, this is likely the cause.
	//    Keep in mind that a timeout of 10 does not mean you only get updates every 10s; by the nature of
	//    long-polling, Telegram responds to your request as soon as new messages are available.
	//    When setting this, it is recommended you set your PollingOpts.Timeout value to be slightly bigger (eg, +1).
	GetUpdatesOpts *GetUpdatesOpts
}

func (d *Dispatcher) logf(format string, args ...any) {
	if d.ErrorLog != nil {
		d.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// ProcessUpdate iterates over the list of groups to execute the matching handlers.
// This is also where we recover from any panics that are thrown by user code, to avoid taking down the bot.
func (d *Dispatcher) processUpdate(u *Update) (err error) {
	s, err := d.getSession(u.GetChatId())
	if err != nil {
		return err
	}

	if s.limiter != nil && !s.limiter.Allow() {
		d.logf("dropped update due to rate limit: update_id=%d, chat_id=%d", u.UpdateId, u.GetChatId())
		return
	}

	s.bot.Update(u)
	return nil
}

// Poll starts polling updates from telegram using getUpdates long-polling.
// See PollingOpts for optional values to set in production environments.
func (d *Dispatcher) Poll(b *Bot, opts *PollingOpts) error {
	if d.shuttingDown() {
		return ErrDispatcherClosed
	}

	if d.Concurrent <= 0 {
		d.Concurrent = runtime.GOMAXPROCS(0)
	}

	if d.NewUpdater == nil {
		return ErrDispatcherNilUpdaterInput
	}

	d.current.updateChan = make(chan *Update, 100)
	d.current.sem = make(chan struct{}, d.Concurrent)

	if opts != nil {
		if opts.EnableWebhookDeletion || opts.DropPendingUpdates {
			// For polling to work, we want to make sure we don't have an existing webhook.
			// Extra perk - we can also use this to drop pending updates!
			_, err := b.DeleteWebhook(&DeleteWebhookOpts{
				DropPendingUpdates: opts.DropPendingUpdates,
				RequestOpts:        opts.GetUpdatesOpts.RequestOpts,
			})
			if err != nil {
				return fmt.Errorf("failed to delete webhook: %w", err)
			}
		}
	}

	d.wg.Go(func() {
		var ctx context.Context
		ctx, d.current.cancel = context.WithCancel(context.Background())
		d.pollingLoop(ctx, opts.GetUpdatesOpts)
	})

	for upd := range d.current.updateChan {
		d.current.sem <- struct{}{}
		d.wg.Go(func() {
			defer func() {
				if pv := recover(); pv != nil {
					stack := debug.Stack()
					d.ErrorLog.Printf(
						"panic recovered in worker: \n%v\nSTACK TRACE:\n%s", pv, stack,
					)
				}
				<-d.current.sem
			}()
			err := d.processUpdate(upd)
			if err != nil {
				if d.UnhandledErrFunc != nil {
					d.UnhandledErrFunc(err)
				} else {
					d.logf("failed to process update: %s", err.Error())
				}
			}
		})
	}

	for n := d.Concurrent; n > 0; n-- {
		d.current.sem <- struct{}{}
	}

	return ErrDispatcherClosed
}

func (d *Dispatcher) WorkersInFlight() int {
	return len(d.current.sem)
}

func (d *Dispatcher) WorkersAvailable() int {
	return cap(d.current.sem) - len(d.current.sem)
}

func (d *Dispatcher) WorkerCapacity() int {
	return cap(d.current.sem)
}

func (d *Dispatcher) pollingLoop(ctx context.Context, opts *GetUpdatesOpts) {
	for {
		if d.shuttingDown() {
			return
		}
		updates, err := d.Bot.GetUpdatesWithContext(ctx, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if d.UnhandledErrFunc != nil {
				d.UnhandledErrFunc(err)
			} else {
				d.logf("failed to get updates; sleeping 1s: %s", err.Error())
				time.Sleep(time.Second)
			}
			continue
		}

		if len(updates) == 0 {
			continue
		}

		opts.Offset = updates[len(updates)-1].UpdateId + 1
		for _, upd := range updates {
			select {
			case d.current.updateChan <- &upd:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (d *Dispatcher) getSession(chatID int64) (*session, error) {
	if actual, ok := d.current.sessions.Load(chatID); ok {
		s, ok := actual.(*session)
		if !ok {
			return nil, fmt.Errorf("invalid session type for chat %d: %T", chatID, actual)
		}
		return s, nil
	}

	s := &session{
		bot:     d.NewUpdater(chatID),
		limiter: d.Limiter,
	}

	d.current.sessions.Store(chatID, s)
	return s, nil
}

func (d *Dispatcher) Shutdown(ctx context.Context) error {
	d.inShutdown.Store(true)

	if d.current.cancel != nil {
		d.current.cancel()
	}

	d.mu.Lock()
	for _, f := range d.onShutdown {
		go f()
	}
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()

		if d.current.updateChan != nil {
			close(d.current.updateChan)
		}

		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (d *Dispatcher) shuttingDown() bool {
	return d.inShutdown.Load()
}

func (d *Dispatcher) RegisterOnShutdown(f func()) {
	d.mu.Lock()
	d.onShutdown = append(d.onShutdown, f)
	d.mu.Unlock()
}

// func cleanedStack() string {
// 	return strings.Join(strings.Split(string(debug.Stack()), "\n")[4:], "\n")
// }
