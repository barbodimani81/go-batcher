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

	// ticker timeout
	timeout  time.Duration
	interval time.Duration
	// context timeout
	handler handlerFunc[T]
	ticker  *time.Ticker

	done      chan struct{}
	flushCh   chan struct{}
	tickerCh  <-chan time.Time
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
		tickerCh:  nil,
	}
	go c.run()
	return c, nil
}

func (c *Cargo[T]) run() {
	for {
		select {
		case <-c.tickerCh:
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot size based flush: %v", err)
			}
		case <-c.flushCh:
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot timeout flush: %v", err)
			}
			c.ticker.Reset(c.timeout)
		case <-c.done:
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot final flush: %v", err)
			}
			c.ticker.Stop()
			return
		}
	}
}

// Add adds one item
func (c *Cargo[T]) Add(item T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.batch) == 0 {
		c.ticker = time.NewTicker(c.timeout)
		c.tickerCh = c.ticker.C
	}

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
	c.mu.Lock()
	defer c.mu.Unlock()
	flushCtx, cancel := context.WithTimeout(ctx, c.interval)
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
	c.closeOnce.Do(func() {
		close(c.done)
	})
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
