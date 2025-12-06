package cargo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkCargoThroughput measures raw throughput
func BenchmarkCargoThroughput(b *testing.B) {
	handler := func(ctx context.Context, batch []int) error {
		// Simulate minimal processing
		return nil
	}

	c, err := NewCargo[int](1000, 100*time.Millisecond, handler)
	if err != nil {
		b.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Add(i)
	}
}

// BenchmarkCargo1000RPS simulates 1000 requests per second
func BenchmarkCargo1000RPS(b *testing.B) {
	var processed atomic.Int64

	handler := func(ctx context.Context, batch []int) error {
		processed.Add(int64(len(batch)))
		return nil
	}

	c, err := NewCargo[int](100, 50*time.Millisecond, handler)
	if err != nil {
		b.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	// 1000 RPS = 1 request per millisecond
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		<-ticker.C
		_ = c.Add(i)
	}

	elapsed := time.Since(start)
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "ops/sec")
	b.ReportMetric(float64(processed.Load()), "processed")
}

// BenchmarkCargoConcurrent tests concurrent load
func BenchmarkCargoConcurrent(b *testing.B) {
	handler := func(ctx context.Context, batch []int) error {
		// Simulate some work
		time.Sleep(time.Microsecond)
		return nil
	}

	c, err := NewCargo[int](500, 50*time.Millisecond, handler)
	if err != nil {
		b.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = c.Add(i)
			i++
		}
	})
}

// BenchmarkCargoHighLoad simulates high load scenarios
func BenchmarkCargoHighLoad(b *testing.B) {
	benchmarks := []struct {
		name      string
		batchSize int
		timeout   time.Duration
		workers   int
	}{
		{"Small_Fast", 100, 10 * time.Millisecond, 10},
		{"Medium_Balanced", 500, 50 * time.Millisecond, 20},
		{"Large_Slow", 1000, 100 * time.Millisecond, 50},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			var processed atomic.Int64

			handler := func(ctx context.Context, batch []int) error {
				processed.Add(int64(len(batch)))
				return nil
			}

			c, err := NewCargo[int](bm.batchSize, bm.timeout, handler)
			if err != nil {
				b.Fatalf("NewCargo error: %v", err)
			}
			defer c.Close()

			b.ResetTimer()
			start := time.Now()

			var wg sync.WaitGroup
			itemsPerWorker := b.N / bm.workers

			for w := 0; w < bm.workers; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; i < itemsPerWorker; i++ {
						_ = c.Add(workerID*itemsPerWorker + i)
					}
				}(w)
			}

			wg.Wait()
			elapsed := time.Since(start)

			b.ReportMetric(float64(b.N)/elapsed.Seconds(), "ops/sec")
			b.ReportMetric(float64(processed.Load()), "processed")
		})
	}
}
