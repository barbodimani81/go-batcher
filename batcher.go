package cargo

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type handlerFunc[T any] func(ctx context.Context, batch []T) error

type Cargo[T any] struct {
	mu        sync.Mutex
	batch     []T
	batchSize int

	timeout time.Duration
	handler handlerFunc[T]

	done      chan struct{}
	flushCh   chan struct{}
	stopped   chan struct{}
	flushing  sync.Mutex
	closeOnce sync.Once
}

func NewCargo[T any](size int, timeout time.Duration, fn func(ctx context.Context, batch []T) error) (*Cargo[T], error) {
	if err := configValidation(size, timeout, fn); err != nil {
		return nil, err
	}

	c := &Cargo[T]{
		batch:     make([]T, 0, size),
		batchSize: size,
		timeout:   timeout,
		handler:   fn,
		done:      make(chan struct{}),
		flushCh:   make(chan struct{}, 1),
		stopped:   make(chan struct{}),
	}

	go c.run()
	return c, nil
}

func (c *Cargo[T]) run() {
	ticker := time.NewTicker(c.timeout)
	defer ticker.Stop()
	defer close(c.stopped)
	for {
		select {
		case <-ticker.C:
			_ = c.flush(context.Background())
			ticker.Reset(c.timeout)
		case <-c.flushCh:
			_ = c.flush(context.Background())
			ticker.Reset(c.timeout)
		case <-c.done:
			_ = c.flush(context.Background())
			return
		}
	}
}

// Add adds one item
func (c *Cargo[T]) Add(item T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.done:
		return fmt.Errorf("cargo closed")
	default:
	}

	c.batch = append(c.batch, item)
	if len(c.batch) >= c.batchSize {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// flush is the internal flush with proper synchronization
func (c *Cargo[T]) flush(ctx context.Context) error {
	c.flushing.Lock()
	defer c.flushing.Unlock()

	c.mu.Lock()
	if len(c.batch) == 0 {
		c.mu.Unlock()
		return nil
	}

	b := c.batch
	c.batch = make([]T, 0, c.batchSize)
	c.mu.Unlock()

	return c.handler(ctx, b)
}

// Flush flushes the current batch (public API)
func (c *Cargo[T]) Flush(ctx context.Context) error {
	return c.flush(ctx)
}

func (c *Cargo[T]) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	<-c.stopped
	return nil
}

func configValidation[T any](size int, timeout time.Duration, fn handlerFunc[T]) error {
	if size <= 0 {
		return fmt.Errorf("batch size must be greater than zero")
	}
	if timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}
	if fn == nil {
		return fmt.Errorf("handler func cannot be empty")
	}
	return nil
}
