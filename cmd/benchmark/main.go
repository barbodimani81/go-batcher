package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
)

func main() {
	rps := flag.Int("rps", 1000, "requests per second to simulate")
	duration := flag.Duration("duration", 10*time.Second, "how long to run the test")
	batchSize := flag.Int("batch-size", 100, "batch size")
	timeout := flag.Duration("timeout", 50*time.Millisecond, "flush timeout")
	flag.Parse()

	var (
		added     atomic.Int64
		processed atomic.Int64
		batches   atomic.Int64
		errors    atomic.Int64
	)

	handler := func(ctx context.Context, batch []int) error {
		batches.Add(1)
		processed.Add(int64(len(batch)))
		// Simulate some processing time
		time.Sleep(time.Microsecond * 100)
		return nil
	}

	c, err := cargo.NewCargo(*batchSize, *timeout, handler)
	if err != nil {
		log.Fatalf("Failed to create cargo: %v", err)
	}

	fmt.Printf("Starting load test:\n")
	fmt.Printf("  Target RPS: %d\n", *rps)
	fmt.Printf("  Duration: %v\n", *duration)
	fmt.Printf("  Batch Size: %d\n", *batchSize)
	fmt.Printf("  Timeout: %v\n\n", *timeout)

	// Calculate interval between requests
	interval := time.Second / time.Duration(*rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	done := make(chan struct{})
	start := time.Now()

	// Stats reporter
	go func() {
		statsTicker := time.NewTicker(time.Second)
		defer statsTicker.Stop()
		lastAdded := int64(0)
		lastProcessed := int64(0)

		for {
			select {
			case <-statsTicker.C:
				currentAdded := added.Load()
				currentProcessed := processed.Load()
				addedRate := currentAdded - lastAdded
				processedRate := currentProcessed - lastProcessed

				fmt.Printf("[%6.1fs] Added: %d/s | Processed: %d/s | Batches: %d | Errors: %d\n",
					time.Since(start).Seconds(),
					addedRate,
					processedRate,
					batches.Load(),
					errors.Load(),
				)

				lastAdded = currentAdded
				lastProcessed = currentProcessed
			case <-done:
				return
			}
		}
	}()

	// Request generator
	go func() {
		i := 0
		for {
			select {
			case <-ticker.C:
				if err := c.Add(i); err != nil {
					errors.Add(1)
				} else {
					added.Add(1)
				}
				i++
			case <-done:
				return
			}
		}
	}()

	// Wait for duration
	time.Sleep(*duration)
	close(done)

	// Close and wait for final flush
	if err := c.Close(); err != nil {
		log.Printf("Error closing cargo: %v", err)
	}

	elapsed := time.Since(start)

	// Final stats
	fmt.Printf("\n=== Final Results ===\n")
	fmt.Printf("Duration: %v\n", elapsed)
	fmt.Printf("Total Added: %d\n", added.Load())
	fmt.Printf("Total Processed: %d\n", processed.Load())
	fmt.Printf("Total Batches: %d\n", batches.Load())
	fmt.Printf("Total Errors: %d\n", errors.Load())
	fmt.Printf("Actual RPS: %.2f\n", float64(added.Load())/elapsed.Seconds())
	fmt.Printf("Avg Batch Size: %.2f\n", float64(processed.Load())/float64(batches.Load()))
	fmt.Printf("Processing Rate: %.2f items/sec\n", float64(processed.Load())/elapsed.Seconds())

	if processed.Load() == added.Load() {
		fmt.Printf("\n✅ SUCCESS: All items processed!\n")
	} else {
		fmt.Printf("\n⚠️  WARNING: %d items not processed\n", added.Load()-processed.Load())
	}
}
