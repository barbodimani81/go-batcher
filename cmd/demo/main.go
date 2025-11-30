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

	// Flags.
	objectsCount := flag.Int("count", 1000, "number of items to generate")
	batchSizeLimit := flag.Int("batch-size", 10, "number of items per batch")
	timeout := flag.Duration("timeout", 2*time.Second, "flush timeout")
	goroutines := flag.Int("workers", 4, "number of worker goroutines")
	flag.Parse()

	var generated int64
	var flushed int64

	// 1. Create the batcher.
	cfg := cargo.Config{
		BatchSize: *batchSizeLimit,
		Timeout:   *timeout,
		Handler: func(ctx context.Context, batch []any) error {
			atomic.AddInt64(&flushed, int64(len(batch)))
			// In a real app, you would insert into DB, call an API, etc.
			return nil
		},
	}

	c, err := cargo.New(cfg)
	if err != nil {
		log.Fatalf("failed to create cargo: %v", err)
	}

	// 2. Start generator.
	ch := generator.Generate(*objectsCount)

	// 3. Start workers.
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < *goroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for b := range ch {
				atomic.AddInt64(&generated, 1)

				// For demo purposes we just pass the raw []byte.
				// In a real use-case you might decode it first.
				if err := c.Add(ctx, b); err != nil {
					log.Printf("Add error: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()

	// 4. final flush + close.
	_ = c.Flush(context.Background())
	_ = c.Close(context.Background())

	// 5. print totals.
	log.Printf("Generated items: %d", atomic.LoadInt64(&generated))
	log.Printf("Flushed items:   %d", atomic.LoadInt64(&flushed))

	// duration.
	elapsed := time.Since(start)
	log.Printf("Duration: %v", elapsed)
}
