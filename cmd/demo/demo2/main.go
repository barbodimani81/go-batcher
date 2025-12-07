package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
	"github.com/barbodimani81/go-batcher/cmd/demo/generator"
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
	c, err := cargo.NewCargo(100, 2, 5, handler)
	if err != nil {
		panic(err)
	}

	//time.Sleep(5 * time.Second)

	c.Close()

	fmt.Println("ended")
	os.Exit(0)
	log.Println("item add")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Add(ctx, item); err != nil {
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
