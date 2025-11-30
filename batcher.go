package cargo

import (
	"context"
	"errors"
	"sync"
	"time"
)

// HandlerFunc is called whenever a batch is flushed.
// The batch slice must be treated as read-only and must not be retained.
type HandlerFunc func(ctx context.Context, batch []any) error

// Config holds the configuration for Cargo.
type Config struct {
	// BatchSize is the number of items after which a flush is triggered.
	// If BatchSize <= 0, size-based flushing is disabled.
	BatchSize int

	// Timeout is the maximum amount of time to wait before flushing
	// the current batch. If Timeout <= 0, time-based flushing is disabled.
	Timeout time.Duration

	// Handler is called on every flush with the current batch.
	// Handler must be safe for concurrent use.
	Handler HandlerFunc
}

// Cargo is an in-memory batcher. It is safe for concurrent use.
type Cargo struct {
	mu     sync.Mutex
	cfg    Config
	buffer []any
	timer  *time.Timer
	closed bool
}

// Errors returned by Cargo.
var (
	ErrNilHandler   = errors.New("cargo: handler must not be nil")
	ErrClosed       = errors.New("cargo: batcher is closed")
	ErrInvalidBatch = errors.New("cargo: batch size must be > 0 when enabled")
)

// New creates a new Cargo batcher with the given configuration.
func New(cfg Config) (*Cargo, error) {
	if cfg.Handler == nil {
		return nil, ErrNilHandler
	}
	if cfg.BatchSize == 0 && cfg.Timeout <= 0 {
		return nil, errors.New("cargo: either BatchSize or Timeout must be set")
	}
	if cfg.BatchSize < 0 {
		return nil, ErrInvalidBatch
	}

	return &Cargo{
		cfg: cfg,
	}, nil
}

// Add adds a single item to the batch.
//
// It may trigger a flush if the batch size threshold is reached.
// The provided context is used for the handler if this call triggers
// a size-based flush. Time-based flushes use context.Background.
func (c *Cargo) Add(ctx context.Context, item any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrClosed
	}

	// Ensure timer exists if timeout is enabled and no timer is running.
	if c.cfg.Timeout > 0 && c.timer == nil {
		c.startTimerLocked()
	}

	c.buffer = append(c.buffer, item)

	if c.cfg.BatchSize > 0 && len(c.buffer) >= c.cfg.BatchSize {
		return c.flushLocked(ctx)
	}

	return nil
}

// Flush flushes the current batch immediately.
// If the batch is empty, Flush is a no-op.
func (c *Cargo) Flush(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.flushLocked(ctx)
}

// Close marks the batcher as closed and flushes any remaining items.
// After Close, Add will return ErrClosed.
func (c *Cargo) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true

	// Stop any running timer.
	c.stopTimerLocked()

	err := c.flushLocked(ctx)
	c.mu.Unlock()

	return err
}

// flushLocked must be called with c.mu held.
func (c *Cargo) flushLocked(ctx context.Context) error {
	if len(c.buffer) == 0 {
		return nil
	}

	// Stop timer once we flush (we'll recreate it on next Add if needed).
	c.stopTimerLocked()

	// Copy the batch so the handler can't mutate our internal slice.
	batch := make([]any, len(c.buffer))
	copy(batch, c.buffer)

	// Reset buffer.
	c.buffer = c.buffer[:0]

	// Call handler outside of the locked section? Here we keep it simple
	// and call while holding the lock to avoid reentrancy issues.
	// For heavy handlers, you might want to release the lock before calling.
	return c.cfg.Handler(ctx, batch)
}

func (c *Cargo) stopTimerLocked() {
	if c.timer != nil {
		if c.timer.Stop() {
			// timer successfully stopped
		}
		c.timer = nil
	}
}

func (c *Cargo) startTimerLocked() {
	timeout := c.cfg.Timeout

	c.timer = time.AfterFunc(timeout, func() {
		// Time-based flush cannot use the caller's context, so we use Background.
		_ = c.Flush(context.Background())
	})
}
