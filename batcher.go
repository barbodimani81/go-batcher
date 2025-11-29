package cargo

import (
	"context"
	"errors"
	"sync"
	"time"
)

// HandlerFunc is called whenever a batch is flushed.
// The batch slice must be treated as read-only and not retained.
type HandlerFunc func(ctx context.Context, batch []any) error

// Config holds the configuration for Cargo.
type Config struct {
	// BatchSize is the number of items after which a flush is triggered.
	BatchSize int

	// Timeout is the maximum time items can sit in the batch
	// before a flush is triggered. If Timeout <= 0, only size-based
	// flushing is used.
	Timeout time.Duration

	// HandlerFunc is called with each flushed batch.
	HandlerFunc HandlerFunc
}

var (
	// ErrClosed is returned when Add is called after Close.
	ErrClosed = errors.New("cargo: closed")
)

// Cargo accumulates items in memory and flushes them by size or timeout.
type Cargo struct {
	mu     sync.Mutex
	batch  []any
	cfg    Config
	timer  *time.Timer
	closed bool
}

// New creates a new Cargo with the given configuration.
func New(cfg Config) (*Cargo, error) {
	if cfg.BatchSize <= 0 {
		return nil, errors.New("cargo: batch size must be greater than zero")
	}
	if cfg.HandlerFunc == nil {
		return nil, errors.New("cargo: handler func cannot be nil")
	}
	// Timeout may be zero/negative: that just means "no time-based flush".

	return &Cargo{
		batch: make([]any, 0, cfg.BatchSize),
		cfg:   cfg,
	}, nil
}

// Add adds one item to the batch.
//
// It may trigger a size-based flush. In that case the provided ctx is passed
// to the HandlerFunc. For time-based flushes, context.Background is used.
func (c *Cargo) Add(ctx context.Context, item any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}

	wasEmpty := len(c.batch) == 0
	c.batch = append(c.batch, item)

	// If we've reached the batch size, flush immediately.
	if len(c.batch) >= c.cfg.BatchSize {
		batch := c.detachBatchLocked()

		// Stop any existing timer; we just flushed.
		c.stopTimerLocked()

		c.mu.Unlock()
		return c.cfg.HandlerFunc(ctx, batch)
	}

	// Only schedule a timeout when we transition from empty -> non-empty.
	if wasEmpty && c.cfg.Timeout > 0 && c.timer == nil {
		c.startTimerLocked()
	}

	c.mu.Unlock()
	return nil
}

// Flush flushes the current batch, if any.
func (c *Cargo) Flush(ctx context.Context) error {
	c.mu.Lock()
	if len(c.batch) == 0 {
		c.mu.Unlock()
		return nil
	}

	batch := c.detachBatchLocked()
	c.stopTimerLocked()
	c.mu.Unlock()

	return c.cfg.HandlerFunc(ctx, batch)
}

// Close marks the cargo as closed and flushes any remaining items.
func (c *Cargo) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	return c.Flush(ctx)
}

// --- internal helpers (must be called with c.mu held) ---

func (c *Cargo) detachBatchLocked() []any {
	if len(c.batch) == 0 {
		return nil
	}
	batch := c.batch
	c.batch = make([]any, 0, c.cfg.BatchSize)
	return batch
}

func (c *Cargo) stopTimerLocked() {
	if c.timer != nil {
		c.timer.Stop()
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
