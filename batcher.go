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

	timeout  time.Duration
	interval time.Duration

	handler handlerFunc[T]
	ticker  *time.Ticker

	done      chan struct{}
	flushCh   chan struct{}
	closeOnce sync.Once
	runWg     sync.WaitGroup
}

func NewCargo[T any](size int, timeout, interval time.Duration, fn func(ctx context.Context, batch []T) error) (*Cargo[T], error) {
	if err := configValidation(size, timeout, interval, fn); err != nil {
		return nil, err
	}

	c := &Cargo[T]{
		batch:     make([]T, 0, size),
		batchSize: size,
		interval:  interval,
		timeout:   timeout,
		handler:   fn,
		done:      make(chan struct{}),
		flushCh:   make(chan struct{}, 1),
		ticker:    time.NewTicker(interval),
	}
	c.ticker.Stop()
	c.runWg.Add(1)
	go c.run()
	return c, nil
}

func (c *Cargo[T]) run() {
	for {
		select {
		case <-c.ticker.C:
			go func() {
				err := c.flush(context.Background())
				if err != nil {
					log.Printf("cannot size based flush: %v", err)
				}
			}()
			c.mu.Lock()
			c.ticker.Stop()
			c.mu.Unlock()
		case <-c.flushCh:
			// TODO: Same issue - context.Background() ignores caller's context
			go func() {
				err := c.flush(context.Background())
				if err != nil {
					log.Printf("cannot interval flush: %v", err)
				}
			}()
			c.mu.Lock()
			c.ticker.Stop()
			c.mu.Unlock()
		case <-c.done:
			// TODO: Final flush also uses Background context - can't respect shutdown deadline
			fmt.Println("done")
			err := c.flush(context.Background())
			if err != nil {
				log.Printf("cannot final flush: %v", err)
			}
			c.mu.Lock()
			if c.ticker != nil {
				c.ticker.Stop()
			}
			c.mu.Unlock()
			c.runWg.Done()
			return
		}
	}
}

func (c *Cargo[T]) Add(ctx context.Context, item T) error {
	c.mu.Lock()

	if len(c.batch) == 0 {
		c.ticker.Reset(c.interval)
	}

	c.batch = append(c.batch, item)

	shouldFlush := len(c.batch) >= c.batchSize
	c.mu.Unlock()

	if shouldFlush {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func (c *Cargo[T]) flush(ctx context.Context) error {
	c.mu.Lock()

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
	c.closeOnce.Do(func() {
		close(c.done)
		c.runWg.Wait()
	})
	return nil
}

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
