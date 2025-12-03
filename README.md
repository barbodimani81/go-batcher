# go-batcher

A lightweight, type-safe batch processor for Go that automatically flushes items based on size or time. Perfect for reducing I/O overhead when writing to databases, APIs, or any system that benefits from batching.

## Why Use This?

Instead of making 1000 individual database inserts, batch them into 10 inserts of 100 items each. This dramatically reduces overhead and improves throughput.

## Features

- **Type-safe**: Uses Go generics for compile-time type safety
- **Dual triggers**: Flush by batch size, timeout, or both
- **Goroutine-safe**: Concurrent `Add()` calls are handled safely
- **Zero dependencies**: Pure Go standard library
- **Simple API**: Just 4 methods to learn

## Installation

```bash
go get github.com/barbodimani81/go-batcher
```

## Quick Start

Here's a complete example that batches integers and prints them:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
)

func main() {
	// Create a batcher that flushes every 10 items or every 2 seconds
	batcher, err := cargo.NewCargo(
		10,                // batch size
		2*time.Second,     // timeout
		func(ctx context.Context, batch []int) error {
			fmt.Printf("Processing batch of %d items: %v\n", len(batch), batch)
			return nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer batcher.Close()

	// Add items - they'll be batched automatically
	for i := 1; i <= 25; i++ {
		if err := batcher.Add(i); err != nil {
			log.Fatal(err)
		}
	}

	// Close flushes any remaining items
}
```

**Output:**
```
Processing batch of 10 items: [1 2 3 4 5 6 7 8 9 10]
Processing batch of 10 items: [11 12 13 14 15 16 17 18 19 20]
Processing batch of 5 items: [21 22 23 24 25]
```

## Step-by-Step Guide

### Step 1: Define Your Item Type

```go
type LogEntry struct {
	Timestamp time.Time
	Message   string
	Level     string
}
```

### Step 2: Create a Handler Function

The handler receives a batch of items and processes them:

```go
func processLogs(ctx context.Context, batch []LogEntry) error {
	// Your batch processing logic here
	// For example: write to database, send to API, etc.
	fmt.Printf("Writing %d log entries to database\n", len(batch))
	
	// Simulate database write
	for _, entry := range batch {
		// db.Insert(entry)
		fmt.Printf("  - [%s] %s\n", entry.Level, entry.Message)
	}
	
	return nil
}
```

### Step 3: Initialize the Batcher

```go
batcher, err := cargo.NewCargo(
	100,              // flush when 100 items collected
	5*time.Second,    // or flush every 5 seconds
	processLogs,      // your handler function
)
if err != nil {
	log.Fatal(err)
}
defer batcher.Close() // Always close to flush remaining items
```

### Step 4: Add Items

```go
// Add items from anywhere in your code
entry := LogEntry{
	Timestamp: time.Now(),
	Message:   "User logged in",
	Level:     "INFO",
}

if err := batcher.Add(entry); err != nil {
	log.Printf("Failed to add entry: %v", err)
}
```

### Step 5: Manual Flush (Optional)

```go
// Force an immediate flush if needed
if err := batcher.Flush(context.Background()); err != nil {
	log.Printf("Flush failed: %v", err)
}
```

## Real-World Example: Database Inserts

Here's a practical example using MongoDB:

```go
package main

import (
	"context"
	"log"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type User struct {
	ID    int    `bson:"id"`
	Name  string `bson:"name"`
	Email string `bson:"email"`
}

func main() {
	// Connect to MongoDB
	client, err := mongo.Connect(context.Background(), 
		options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())

	collection := client.Database("myapp").Collection("users")

	// Create batcher with MongoDB insert handler
	batcher, err := cargo.NewCargo(
		1000,             // batch 1000 users at a time
		10*time.Second,   // or flush every 10 seconds
		func(ctx context.Context, batch []User) error {
			// Convert to []interface{} for MongoDB
			docs := make([]interface{}, len(batch))
			for i, user := range batch {
				docs[i] = user
			}
			
			// Bulk insert
			_, err := collection.InsertMany(ctx, docs)
			if err != nil {
				log.Printf("Insert failed: %v", err)
				return err
			}
			
			log.Printf("Inserted %d users", len(batch))
			return nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer batcher.Close()

	// Add users (could be from API, CSV, etc.)
	for i := 1; i <= 5000; i++ {
		user := User{
			ID:    i,
			Name:  fmt.Sprintf("User %d", i),
			Email: fmt.Sprintf("user%d@example.com", i),
		}
		
		if err := batcher.Add(user); err != nil {
			log.Printf("Failed to add user: %v", err)
		}
	}
	
	// Close() will flush remaining users
	log.Println("All users processed")
}
```

## API Reference

### Creating a Batcher

```go
func NewCargo[T any](
	batchSize int,
	timeout time.Duration,
	handler func(ctx context.Context, batch []T) error,
) (*Cargo[T], error)
```

**Parameters:**
- `batchSize`: Number of items to collect before flushing (must be > 0)
- `timeout`: Maximum time to wait before flushing (must be > 0)
- `handler`: Function that processes each batch

**Returns:** A new `Cargo` instance or an error if parameters are invalid.

### Methods

```go
// Add an item to the batch
func (c *Cargo[T]) Add(item T) error

// Manually flush the current batch
func (c *Cargo[T]) Flush(ctx context.Context) error

// Close the batcher and flush remaining items
func (c *Cargo[T]) Close() error
```

## How It Works

1. Items are added to an internal buffer via `Add()`
2. When the buffer reaches `batchSize`, it automatically flushes
3. If `timeout` elapses before reaching `batchSize`, it flushes anyway
4. Your handler function receives the batch and processes it
5. The buffer is cleared and the cycle repeats

## Concurrency

The batcher is safe for concurrent use:

```go
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
	wg.Add(1)
	go func(id int) {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			batcher.Add(fmt.Sprintf("worker-%d-item-%d", id, j))
		}
	}(i)
}

wg.Wait()
batcher.Close()
```

## Common Use Cases

- **Database writes**: Batch inserts/updates to reduce round trips
- **API calls**: Group multiple requests into bulk operations
- **Log aggregation**: Collect logs before writing to disk/service
- **Metrics collection**: Batch metrics before sending to monitoring systems
- **Message queues**: Group messages before publishing
- **File I/O**: Batch writes to reduce system calls

## Error Handling

If your handler returns an error, the batch is lost. Implement retry logic in your handler if needed:

```go
handler := func(ctx context.Context, batch []Item) error {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if err := db.InsertMany(batch); err == nil {
			return nil
		}
		time.Sleep(time.Second * time.Duration(i+1))
	}
	return fmt.Errorf("failed after %d retries", maxRetries)
}
```

## License

MIT License - see LICENSE file for details
