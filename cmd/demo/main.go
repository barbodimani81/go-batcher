package main

import (
	"context"
	"flag"
	"log"
	"sync"
	"sync/atomic"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
	"github.com/barbodimani81/go-batcher/cmd/demo/generator"
)

func main() {
	start := time.Now()

	objectsCount := flag.Int("count", 100, "number of objects to generate")
	batchSize := flag.Int("batch-size", 10, "batch size")
	timeout := flag.Duration("timeout", 2*time.Second, "flush timeout")
	workers := flag.Int("workers", 4, "number of worker goroutines")
	flag.Parse()

	var generated int64
	var flushed int64

	// 1. generator
	ch := generator.ItemGenerator(*objectsCount)

	// 2. cargo / batcher
	c, err := cargo.New(cargo.Config{
		BatchSize: *batchSize,
		Timeout:   *timeout,
		HandlerFunc: func(ctx context.Context, batch []any) error {
			atomic.AddInt64(&flushed, int64(len(batch)))
			// In a real app, you'd do DB inserts or API calls here.
			return nil
		},
	})
	if err != nil {
		log.Fatalf("cargo error: %v", err)
	}

	// 3. worker pool
	var wg sync.WaitGroup
	wg.Add(*workers)

	for i := 0; i < *workers; i++ {
		go func() {
			defer wg.Done()

			// We just use Background here; if this was a real service
			// you'd probably pass a request-scoped context instead.
			ctx := context.Background()

			for msg := range ch {
				atomic.AddInt64(&generated, 1)

				if err := c.Add(ctx, msg); err != nil {
					log.Printf("cargo add error: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	// 4. final flush + close
	_ = c.Flush(context.Background())
	_ = c.Close(context.Background())

	// 5. print totals
	log.Printf("Generated items: %d", atomic.LoadInt64(&generated))
	log.Printf("Flushed items:   %d", atomic.LoadInt64(&flushed))

	// duration
	elapsed := time.Since(start)
	log.Printf("Duration: %v", elapsed)
}
