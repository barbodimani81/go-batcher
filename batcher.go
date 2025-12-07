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

func NewCargo[T any](size int, interval time.Duration, fn func(ctx context.Context, batch []T) error) (*Cargo[T], error) {
	if err := configValidation(size, interval, fn); err != nil {
		return nil, err
	}

	c := &Cargo[T]{
		batch:     make([]T, 0, size),
		batchSize: size,
		interval:  interval,
		handler:   fn,
		done:      make(chan struct{}, 1),
		flushCh:   make(chan struct{}, 1),
		tickerCh:  nil,
	}
	go c.Run()
	return c, nil
}

func (c *Cargo[T]) Run() {
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
				log.Printf("cannot interval flush: %v", err)
			}
			c.Ticker.Reset(c.interval)
		case <-c.done:
			log.Println("done 222")
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
func (c *Cargo[T]) Add(item T) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: CRITICAL - Ticker leak! Every flush cycle creates a new ticker without stopping the old one.
	// This causes goroutine and memory leaks. Need to either:
	// 1. Stop old ticker before creating new one, OR
	// 2. Create ticker once in NewCargo and reset it instead of recreating
	if len(c.batch) == 0 {
		c.Ticker = time.NewTicker(c.interval)
		c.tickerCh = c.Ticker.C
	}

	//select {
	//case <-c.done:
	//	return fmt.Errorf("cargo closed")
	//default:
	//}

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
	// TODO: c.timeout is never initialized in NewCargo, so this creates an immediately cancelled context
	// This breaks all handler operations that respect context cancellation
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
