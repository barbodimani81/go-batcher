package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	cargo "github.com/barbodimani81/go-batcher"
	"github.com/barbodimani81/go-batcher/cmd/demo/generator"
	"github.com/barbodimani81/go-batcher/cmd/demo/repository"
)

func main() {
	objectsCount := flag.Int("count", 1000000, "number of objects to generate")
	batchSize := flag.Int("batch-size", 10000, "batch size")
	timeout := flag.Duration("timeout", 2*time.Second, "flush timeout")
	workers := flag.Int("workers", 6, "number of worker goroutines")
	mongoURI := flag.String("mongo-uri", getEnv("MONGO_URI", "mongodb://mongodb:27017"), "MongoDB connection URI")
	flag.Parse()

	var generated int64

	// mongodb new client and database configurations
	log.Printf("connecting to MongoDB at %s\n", *mongoURI)
	mongoClient, err := mongodb.NewClient(*mongoURI)
	if err != nil {
		log.Fatalf("mongodb error: %v", err)
	}
	defer func() {
		_ = mongoClient.Disconnect(context.Background())
	}()
	// defining new mongo collection
	coll := mongodb.NewRepo(mongoClient)
	log.Println("MongoDB connected successfully")

	// generator putting items in channel
	start := time.Now()
	log.Printf("generating %d objects\n", *objectsCount)
	ch := generator.ItemGenerator(*objectsCount)

	// TODO: generic
	// cargo handler connection to mongo
	handler := func(ctx context.Context, batch []any) error {
		if len(batch) == 0 {
			return nil
		}
		docs := make([]any, len(batch))
		for i, v := range batch {
			item, ok := v.(generator.Item)
			if !ok {
				return fmt.Errorf("invalid format: expected generator.Item, got %T", v)
			}
			docs[i] = item
		}
		// mongo insertion
		log.Printf("inserting batch of %d items\n", len(docs))
		_, err := coll.InsertMany(ctx, docs)
		if err != nil {
			log.Printf("ERROR: insert failed: %v", err)
			return fmt.Errorf("insert docs to mongo failed: %v", err)
		}
		log.Printf("successfully inserted %d items\n", len(docs))
		return nil
	}

	// cargo initialize
	c, err := cargo.NewCargo(*batchSize, *timeout, handler)
	if err != nil {
		log.Fatalf("cargo error: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("close cargo failed: %v", err)
		}
		// duration after all inserts complete
		elapsed := time.Since(start)
		log.Printf("Total duration: %v", elapsed)
	}()

	// worker pool for add to cargo and flush
	var wg sync.WaitGroup
	wg.Add(*workers)

	for i := 0; i < *workers; i++ {
		go func() {
			defer wg.Done()
			for msg := range ch {
				atomic.AddInt64(&generated, 1)
				if err := c.Add(msg); err != nil {
					log.Printf("cargo add error: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	log.Printf("Generated items: %d", atomic.LoadInt64(&generated))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
