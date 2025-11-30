# go-batcher

Lightweight in-memory batcher for Go.

It collects items and flushes them in batches, triggered by **batch size**, **timeout**, or both. Designed for reducing I/O overhead such as database writes or API calls.

---

## Features

* Flush by size, timeout, or both
* Goroutine-safe
* Minimal API
* No external dependencies

---

## Installation

```bash
  go get github.com/barbodimani81/go-batcher
```

---

## Quick Start

```go
package main

import (
	"context"
	"log"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
)

func main() {
	cfg := cargo.Config{
		BatchSize: 10,
		Timeout:   2 * time.Second,
		Handler: func(ctx context.Context, batch []any) error {
			log.Printf("flushed %d items", len(batch))
			return nil
		},
	}

	c, err := cargo.New(cfg)
	if err != nil {
		log.Fatalf("create error: %v", err)
	}

	ctx := context.Background()

	for i := 0; i < 25; i++ {
		if err := c.Add(ctx, i); err != nil {
			log.Fatalf("add error: %v", err)
		}
	}

	if err := c.Close(ctx); err != nil {
		log.Fatalf("close error: %v", err)
	}
}
```

---

## Concepts

### Config

```go
type Config struct {
	BatchSize int
	Timeout   time.Duration
	Handler   HandlerFunc
}
```

* `BatchSize > 0`: flush when buffer reaches this size
* `Timeout > 0`: flush after this duration
* At least one must be enabled

### Handler

```go
type HandlerFunc func(ctx context.Context, batch []any) error
```

You receive the collected items as a slice. Treat it as read-only.

### Cargo Methods

```go
func New(cfg Config) (*Cargo, error)
func (c *Cargo) Add(ctx context.Context, item any) error
func (c *Cargo) Flush(ctx context.Context) error
func (c *Cargo) Close(ctx context.Context) error
```

* `Add` is goroutine-safe
* `Flush` forces a flush
* `Close` flushes remaining items and disables the batcher

---

## Demo

A runnable example exists in `cmd/demo`.

Run it:

```bash
  go run ./cmd/demo \
  -count=1000 \
  -batch-size=10 \
  -timeout=2s \
  -workers=4
```

Example output:

```
Generated items: 1000
Flushed items:   1000
Duration: 5.8s
```

---

## When It Helps

Use it when many small operations can be grouped:

* DB inserts
* API requests
* Logging pipelines
* Stream processing
