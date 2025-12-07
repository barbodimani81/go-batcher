package main

import (
	"context"
	cargo "github.com/barbodimani81/go-batcher"
	"github.com/barbodimani81/go-batcher/cmd/demo/generator"
	"log"
	"time"
)

func main() {
	item := generator.Item{
		ID:     1,
		Age:    22,
		Status: false,
	}

	log.Println("Starting demo")
	handler := func(ctx context.Context, batch []generator.Item) error {
		return nil
	}

	log.Println("new cargo")
	c, err := cargo.NewCargo(100, 2, handler)
	if err != nil {
		panic(err)
	}

	log.Println("item add")
	if err := c.Add(item); err != nil {
		print(err)
	}
	c.Ticker.Stop()

	time.Sleep(1 * time.Second)
	log.Println("close")
	if err := c.Close(); err != nil {
		print(err)
	}

	log.Println("done")
}
