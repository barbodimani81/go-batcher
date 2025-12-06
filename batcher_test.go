package cargo

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- constructor validation ---

func TestNewCargoValidation(t *testing.T) {
	handler := func(ctx context.Context, batch []int) error { return nil }

	tests := []struct {
		name    string
		size    int
		timeout time.Duration
		fn      handlerFunc[int]
		wantErr bool
	}{
		{"invalid size", 0, time.Second, handler, true},
		{"invalid timeout", 10, 0, handler, true},
		{"nil handler", 10, time.Second, nil, true},
		{"valid config", 10, time.Second, handler, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewCargo[int](tt.size, tt.timeout, tt.fn)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c == nil {
				t.Fatalf("NewCargo returned nil Cargo")
			}
		})
	}
}

// --- Add + Close basic behavior ---

func TestAddAndClose(t *testing.T) {
	handler := func(ctx context.Context, batch []int) error { return nil }

	c, err := NewCargo[int](2, time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}

	if err := c.Add(1); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if err := c.Add(2); err == nil {
		t.Fatalf("expected Add to fail after Close")
	}
}

// --- batch-size flush logic ---

func TestFlushOnBatchSize(t *testing.T) {
	batchCh := make(chan []int, 1)

	handler := func(ctx context.Context, batch []int) error {
		// copy slice so internal reuse doesn't affect the test
		cp := append([]int(nil), batch...)
		batchCh <- cp
		return nil
	}

	c, err := NewCargo[int](3, 5*time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	if err := c.Add(1); err != nil {
		t.Fatalf("Add(1) error: %v", err)
	}
	if err := c.Add(2); err != nil {
		t.Fatalf("Add(2) error: %v", err)
	}
	if err := c.Add(3); err != nil {
		t.Fatalf("Add(3) error: %v", err)
	}

	select {
	case batch := <-batchCh:
		if len(batch) != 3 {
			t.Fatalf("expected batch size 3, got %d", len(batch))
		}
		if batch[0] != 1 || batch[1] != 2 || batch[2] != 3 {
			t.Fatalf("unexpected batch contents: %#v", batch)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for batch-size flush")
	}
}

// --- timeout-based flush logic ---

func TestFlushOnTimeout(t *testing.T) {
	batchCh := make(chan []int, 1)

	handler := func(ctx context.Context, batch []int) error {
		cp := append([]int(nil), batch...)
		batchCh <- cp
		return nil
	}

	timeout := 50 * time.Millisecond

	c, err := NewCargo[int](10, timeout, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	if err := c.Add(10); err != nil {
		t.Fatalf("Add(10) error: %v", err)
	}
	if err := c.Add(20); err != nil {
		t.Fatalf("Add(20) error: %v", err)
	}

	select {
	case batch := <-batchCh:
		if len(batch) != 2 {
			t.Fatalf("expected batch size 2, got %d", len(batch))
		}
	case <-time.After(5 * timeout):
		t.Fatalf("timed out waiting for timeout flush")
	}
}

// --- basic handler execution (just to be explicit) ---

func TestHandlerIsCalled(t *testing.T) {
	var (
		mu        sync.Mutex
		calls     int
		lastBatch []int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		lastBatch = append([]int(nil), batch...)
		return nil
	}

	c, err := NewCargo[int](2, 100*time.Millisecond, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	if err := c.Add(42); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := c.Add(43); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if calls == 0 {
		t.Fatalf("handler was never called")
	}
	if len(lastBatch) == 0 {
		t.Fatalf("handler received empty batch")
	}
}

// --- concurrent Add usage ---

func TestConcurrentAdd(t *testing.T) {
	const (
		nItems  = 100
		batchSz = 10
		timeout = 100 * time.Millisecond
	)

	var (
		mu     sync.Mutex
		values []int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		values = append(values, batch...)
		return nil
	}

	c, err := NewCargo[int](batchSz, timeout, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	var wg sync.WaitGroup
	wg.Add(nItems)

	for i := 0; i < nItems; i++ {
		i := i
		go func() {
			defer wg.Done()
			_ = c.Add(i)
		}()
	}

	wg.Wait()

	time.Sleep(2 * timeout)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(values) < nItems {
		t.Fatalf("expected at least %d items flushed, got %d", nItems, len(values))
	}
}

// --- ticker-based repeated flush (indirectly checks reset/continued operation) ---

func TestRepeatedTimeoutFlushes(t *testing.T) {
	type call struct {
		at    time.Time
		batch []int
	}

	var (
		mu    sync.Mutex
		calls []call
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		cp := append([]int(nil), batch...)
		calls = append(calls, call{
			at:    time.Now(),
			batch: cp,
		})
		return nil
	}

	timeout := 40 * time.Millisecond

	c, err := NewCargo[int](100, timeout, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	if err := c.Add(1); err != nil {
		t.Fatalf("Add(1) error: %v", err)
	}

	time.Sleep(3 * timeout)

	if err := c.Add(2); err != nil {
		t.Fatalf("Add(2) error: %v", err)
	}

	time.Sleep(3 * timeout)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(calls) < 2 {
		t.Fatalf("expected at least 2 timeout-based flushes, got %d", len(calls))
	}

	first := calls[0]
	second := calls[1]
	delta := second.at.Sub(first.at)

	if delta < timeout/2 {
		t.Fatalf("second flush came too quickly after first: %v", delta)
	}
}

// --- manual flush ---

func TestManualFlush(t *testing.T) {
	var (
		mu      sync.Mutex
		batches [][]int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		batches = append(batches, append([]int(nil), batch...))
		return nil
	}

	c, err := NewCargo[int](100, 10*time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	// Add items but don't reach batch size or timeout
	for i := 1; i <= 5; i++ {
		if err := c.Add(i); err != nil {
			t.Fatalf("Add(%d) error: %v", i, err)
		}
	}

	// Manually flush
	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}

	if len(batches[0]) != 5 {
		t.Fatalf("expected batch size 5, got %d", len(batches[0]))
	}
}

// --- empty batch flush ---

func TestEmptyBatchFlush(t *testing.T) {
	called := false

	handler := func(ctx context.Context, batch []int) error {
		called = true
		return nil
	}

	c, err := NewCargo[int](10, time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	// Flush without adding anything
	if err := c.Flush(context.Background()); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if called {
		t.Fatalf("handler should not be called for empty batch")
	}
}

// --- handler error handling ---

func TestHandlerError(t *testing.T) {
	expectedErr := fmt.Errorf("handler error")
	var (
		mu        sync.Mutex
		callCount int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		callCount++
		mu.Unlock()
		return expectedErr
	}

	c, err := NewCargo[int](2, time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	if err := c.Add(1); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if err := c.Add(2); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount == 0 {
		t.Fatalf("handler was never called")
	}
}

// --- close idempotency ---

func TestCloseIdempotency(t *testing.T) {
	handler := func(ctx context.Context, batch []int) error { return nil }

	c, err := NewCargo[int](10, time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}

	// Close multiple times should not panic or error
	if err := c.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("third Close error: %v", err)
	}
}

// --- type safety with generics ---

func TestGenericTypes(t *testing.T) {
	t.Run("string type", func(t *testing.T) {
		var (
			mu     sync.Mutex
			result []string
		)

		handler := func(ctx context.Context, batch []string) error {
			mu.Lock()
			defer mu.Unlock()
			result = append(result, batch...)
			return nil
		}

		c, err := NewCargo[string](3, time.Second, handler)
		if err != nil {
			t.Fatalf("NewCargo error: %v", err)
		}
		defer c.Close()

		items := []string{"hello", "world", "test"}
		for _, item := range items {
			if err := c.Add(item); err != nil {
				t.Fatalf("Add error: %v", err)
			}
		}

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		if len(result) != 3 {
			t.Fatalf("expected 3 items, got %d", len(result))
		}
	})

	t.Run("struct type", func(t *testing.T) {
		type User struct {
			ID   int
			Name string
		}

		var (
			mu     sync.Mutex
			result []User
		)

		handler := func(ctx context.Context, batch []User) error {
			mu.Lock()
			defer mu.Unlock()
			result = append(result, batch...)
			return nil
		}

		c, err := NewCargo[User](2, time.Second, handler)
		if err != nil {
			t.Fatalf("NewCargo error: %v", err)
		}
		defer c.Close()

		users := []User{
			{ID: 1, Name: "Alice"},
			{ID: 2, Name: "Bob"},
		}

		for _, user := range users {
			if err := c.Add(user); err != nil {
				t.Fatalf("Add error: %v", err)
			}
		}

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()
		if len(result) != 2 {
			t.Fatalf("expected 2 users, got %d", len(result))
		}

		if result[0].Name != "Alice" || result[1].Name != "Bob" {
			t.Fatalf("unexpected user data: %+v", result)
		}
	})
}

// --- race condition test ---

func TestNoRaceCondition(t *testing.T) {
	var (
		mu    sync.Mutex
		total int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		for _, v := range batch {
			total += v
		}
		return nil
	}

	c, err := NewCargo[int](10, 50*time.Millisecond, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	const goroutines = 50
	const itemsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				_ = c.Add(1)
			}
		}()
	}

	wg.Wait()

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expected := goroutines * itemsPerGoroutine
	if total != expected {
		t.Fatalf("expected total %d, got %d", expected, total)
	}
}

// --- batch size boundary ---

func TestBatchSizeBoundary(t *testing.T) {
	var (
		mu      sync.Mutex
		batches []int
	)

	handler := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		batches = append(batches, len(batch))
		return nil
	}

	batchSize := 5
	c, err := NewCargo[int](batchSize, 10*time.Second, handler)
	if err != nil {
		t.Fatalf("NewCargo error: %v", err)
	}
	defer c.Close()

	// Add exactly batch size items
	for i := 0; i < batchSize; i++ {
		if err := c.Add(i); err != nil {
			t.Fatalf("Add error: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(batches) != 1 || batches[0] != batchSize {
		mu.Unlock()
		t.Fatalf("expected 1 batch of size %d, got %v", batchSize, batches)
	}
	mu.Unlock()

	// Add one more to start new batch
	if err := c.Add(99); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}

	if batches[1] != 1 {
		t.Fatalf("expected second batch size 1, got %d", batches[1])
	}
}
