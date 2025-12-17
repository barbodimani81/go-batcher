package cargonew

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

var ErrFull = fmt.Errorf("cargo buffer is full, system is busy")
var ErrClosed = fmt.Errorf("cargo is closed")

// cargo's handler
type handlerFunc[T any] func(ctx context.Context, batch []T) error

// CargoConfig holds the Configurations for cargo
type CargoConfig[T any] struct {
	maxRetries     int
	failureHandler func(batch []T, err error)
}

// Option is the function signature for applying configuration changes.
type Option[T any] func(*CargoConfig[T])

// WithMaxRetries configures the maximum number of attempts for a batch handler.
func WithMaxRetries[T any](n int) Option[T] {
	return func(cfg *CargoConfig[T]) {
		if n >= 0 {
			cfg.maxRetries = n
		}
	}
}

// WithFailureHandler sets a custom/Users/barbod/Documents/Sazito/Beehive/beehive/pkg/cargonew/cargo.go callback for permanent data loss events.
func WithFailureHandler[T any](fn func(batch []T, err error)) Option[T] {
	return func(cfg *CargoConfig[T]) {
		cfg.failureHandler = fn
	}
}

// Cargo batches data in-memory via an array using mutex
type Cargo[T any] struct {
	mu        sync.Mutex
	batch     []T
	batchSize int
	closed    bool

	timeout    time.Duration
	interval   time.Duration
	maxRetries int

	handler        handlerFunc[T]
	failureHandler func(batch []T, err error)
	timer          *time.Timer

	ctx       context.Context
	cancel    context.CancelFunc
	flushCh   chan struct{}
	closeOnce sync.Once
	runWg     sync.WaitGroup
}

func NewCargo[T any](
	ctx context.Context,
	size int,
	timeout time.Duration,
	interval time.Duration,
	fn handlerFunc[T],
	opts ...Option[T],
) (*Cargo[T], error) {

	if err := configValidation(size, timeout, interval, fn); err != nil {
		return nil, err
	}

	cfg := CargoConfig[T]{
		maxRetries: 0, // Default
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// TODO: context usage?
	rootCtx, cancel := context.WithCancel(ctx)

	// Set a default permanent failure handler if none is provided
	failureHandler := cfg.failureHandler
	if failureHandler == nil {
		failureHandler = func(batch []T, err error) {
			log.Printf("cargo: handler failed permanently. Data lost for batch size %d: %v", len(batch), err)
		}
	}

	// Create and initialize Cargo
	c := &Cargo[T]{
		batch:          make([]T, 0, size),
		batchSize:      size,
		interval:       interval,
		timeout:        timeout,
		maxRetries:     cfg.maxRetries,
		handler:        fn,
		failureHandler: failureHandler,
		ctx:            rootCtx,
		cancel:         cancel,
		flushCh:        make(chan struct{}, 1),
		timer:          time.NewTimer(interval),
		closed:         false,
	}
	c.timer.Stop()
	c.runWg.Add(1)
	go c.run()
	return c, nil
}

// getAndClearBatch copies the batch and resets it
func (c *Cargo[T]) getAndClearBatch() ([]T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.batch) == 0 {
		return nil, false
	}

	b := c.batch
	c.batch = make([]T, 0, c.batchSize)
	return b, true
}

// processBatch handles cargo's timeout
func (c *Cargo[T]) processBatch(ctx context.Context, batch []T) error {
	flushCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	return c.handler(flushCtx, batch)
}

func (c *Cargo[T]) flushAndRetry(source string) {
	batch, ok := c.getAndClearBatch()
	if !ok {
		return
	}

	var lastErr error
	attempts := c.maxRetries + 1

	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := time.Duration(i) * 100 * time.Millisecond
			log.Printf("cargo: handler failed on %s trigger, retrying batch size %d in %v (attempt %d/%d)", source, len(batch), backoff, i+1, attempts)

			select {
			case <-c.ctx.Done():
				log.Printf("cargo: shutdown detected during retry backoff. Abandoning batch.")
				return
			case <-time.After(backoff):
			}
		}

		lastErr = c.processBatch(c.ctx, batch)
		if lastErr == nil {
			return
		}
	}

	// Permanent Failure: Call the custom handler
	c.failureHandler(batch, lastErr)
}

func (c *Cargo[T]) run() {
	for {
		select {
		case <-c.timer.C:
			c.flushAndRetry("interval")

		case <-c.flushCh:
			c.mu.Lock()
			c.timer.Stop()
			c.mu.Unlock()

			c.flushAndRetry("size-based")

		case <-c.ctx.Done():
			c.mu.Lock()
			if c.timer != nil {
				c.timer.Stop()
			}
			c.mu.Unlock()

			if batch, ok := c.getAndClearBatch(); ok {
				// Use Background context to ensure the final batch is processed
				// even though the main context is canceled.
				err := c.processBatch(context.Background(), batch)
				if err != nil {
					log.Printf("cannot final flush: %v", err)
				}
			}
			c.runWg.Done()
			return
		}
	}
}

// Add func adds to batch and calls flush based on batch size
func (c *Cargo[T]) Add(ctx context.Context, item T) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrClosed
	}

	if len(c.batch) >= c.batchSize {
		return ErrFull
	}

	if len(c.batch) == 0 {
		c.timer.Reset(c.interval)
	}

	c.batch = append(c.batch, item)

	if len(c.batch) == c.batchSize {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// configValidation signature is simplified as it only checks positional arguments
func configValidation[T any](size int, timeout, interval time.Duration, fn handlerFunc[T]) error {
	if size <= 0 {
		return fmt.Errorf("batch size must be greater than zero")
	}
	if timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	if fn == nil {
		return fmt.Errorf("handler func cannot be empty")
	}
	return nil
}

func (c *Cargo[T]) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()

		c.cancel()
		c.runWg.Wait()
	})
	return nil
}
