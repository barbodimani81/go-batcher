package cargo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type handlerFunc[T any] func(ctx context.Context, batch []T) error

type Cargo[T any] struct {
	mu        sync.Mutex
	batch     []T
	batchSize int

	// ticker-interval
	timeout  time.Duration
	interval time.Duration
	// context-timeout
	handler handlerFunc[T]
	Ticker  *time.Ticker

	done      chan struct{}
	flushCh   chan struct{}
	tickerCh  <-chan time.Time
	closeOnce sync.Once
}

func NewCargo[T any](size int, timeout, interval time.Duration, fn func(ctx context.Context, batch []T) error) (*Cargo[T], error) {
// TODO: Add flushTimeout parameter to properly initialize c.timeout
// Signature should be: NewCargo[T any](size int, interval time.Duration, flushTimeout time.Duration, fn ...) (*Cargo[T], error)
// This gives flush operations their own independent timeout, separate from caller contexts
func NewCargo[T any](size int, interval time.Duration, fn func(ctx context.Context, batch []T) error) (*Cargo[T], error) {
	if err := configValidation(size, interval, fn); err != nil {
		return nil, err
	}

	c := &Cargo[T]{
		batch:     make([]T, 0, size),
		batchSize: size,
		interval:  interval,
		timeout:   timeout,
		handler:   fn,
		done:      make(chan struct{}, 1),
		flushCh:   make(chan struct{}, 1),
		tickerCh:  nil,
		Ticker:    time.NewTicker(interval),
		// TODO: Initialize timeout here: timeout: flushTimeout,
	}
	c.Ticker.Stop()
	// TODO: API Design Issue - Starting goroutine in constructor causes problems:
	// 1. User has no control over when background work starts
	// 2. Creates race condition if user calls Run() again (see demo/main.go:98)
	// 3. Makes testing harder
	// FIX: Either make Run() unexported (run) so user can't call it, OR
	//      remove this line and let user call Start() explicitly
	// *** run unexported
	go c.run()
	return c, nil
}

// TODO: This should be unexported (run) to prevent users from calling it multiple times
// Currently if user calls Run() again, two goroutines compete for the same channels
// causing race conditions and double-flush bugs
// *** run unexported
func (c *Cargo[T]) run() {
	for {
		select {
		case <-c.tickerCh:
			// TODO: Using context.Background() loses cancellation/deadline from Add() caller
			// Should store context from Add() and use it here for proper propagation
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot size based flush: %v", err)
			}
			c.Ticker.Stop()
		case <-c.flushCh:
			// TODO: Same issue - context.Background() ignores caller's context
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot interval flush: %v", err)
			}
			// TODO: Logic error - Should STOP ticker here, not Reset it.
			// After size-based flush, batch is empty, so ticker should stop.
			// It will be restarted on next Add() when batch goes from empty to 1 item.
			// Currently this keeps ticker running unnecessarily and wastes resources.
			// *** ticker stop in all cases
			c.Ticker.Stop()
		case <-c.done:
			log.Println("done 222")
			// TODO: Final flush also uses Background context - can't respect shutdown deadline
			err := c.flush(context.Background())
			log.Println("before here")
			if err != nil {
				log.Printf("cannot final flush: %v", err)
			}
			log.Println("here")
			if c.Ticker != nil {
				c.Ticker.Stop()
			}
			return
		}
	}
}

// Add adds one item
// TODO: API Design - Should accept context.Context as first parameter
// This would allow:
// 1. Respecting caller's cancellation (don't accept work if request is cancelled)
// 2. Propagating deadlines to the handler
// 3. Passing trace IDs and request metadata for observability
// Signature should be: Add(ctx context.Context, item T) error
// Implementation (Option 4 - Check context at Add time only):
//   select {
//   case <-ctx.Done():
//       return ctx.Err()
//   default:
//   }
// This prevents accepting already-cancelled work without complicating flush logic
func (c *Cargo[T]) Add(item T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: CRITICAL - Ticker leak! Every flush cycle creates a new ticker without stopping the old one.
	// This causes goroutine and memory leaks. Need to either:
	// 1. Stop old ticker before creating new one, OR
	// 2. Create ticker once in NewCargo and reset it instead of recreating
	// *** timer Reset in Add done
	if len(c.batch) == 0 {
		c.Ticker.Reset(c.interval)
	}

	c.batch = append(c.batch, item)
	if len(c.batch) >= c.batchSize {
		select {
		case c.flushCh <- struct{}{}:
			c.Ticker.Reset(c.interval)
		default:
		}
	}
	return nil
}

// flush is the internal flush with proper synchronization
func (c *Cargo[T]) flush(ctx context.Context) error {
	c.mu.Lock()
	// TODO: c.timeout is never initialized in NewCargo, so this creates an immediately cancelled context
	// This breaks all handler operations that respect context cancellation
	// FIX: After adding flushTimeout to NewCargo and initializing c.timeout,
	//      change this to: flushCtx, cancel := context.WithTimeout(context.Background(), c.timeout)
	//      Using Background() makes flush independent of caller contexts (Option 4)
	// *** timeout added to newCargo
	flushCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if len(c.batch) == 0 {
		c.mu.Unlock()
		return nil
	}

	b := c.batch
	c.batch = make([]T, 0, c.batchSize)
	c.mu.Unlock()

	return c.handler(flushCtx, b)
}

func (c *Cargo[T]) Close() error {
	log.Println("cargo closinghhhh")
	c.closeOnce.Do(func() {
		log.Println("cargo closed")
		c.done <- struct{}{}
	})
	log.Println("cargo closed2")
	return nil
}

func configValidation[T any](size int, interval time.Duration, fn handlerFunc[T]) error {
	if size <= 0 {
		return fmt.Errorf("batch size must be greater than zero")
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	if fn == nil {
		return fmt.Errorf("handler func cannot be empty")
	}
	return nil
}
