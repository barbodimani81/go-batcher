package generator

import (
	"encoding/json"
	"log"
	"math/rand"
	"time"
)

type Item struct {
	ID     int  `json:"id"`
	Age    int  `json:"age"`
	Status bool `json:"status"`
}

func init() {
	// Seed for non-deterministic output in demos.
	rand.Seed(time.Now().UnixNano())
}

// ItemGenerator generates n JSON-encoded Items and sends them on a channel.
// The channel is closed when generation is complete.
func ItemGenerator(n int) <-chan []byte {
	ch := make(chan []byte)

	go func() {
		defer close(ch)

		for i := 0; i < n; i++ {
			item := Item{
				ID:     rand.Intn(1_000_000),
				Age:    rand.Intn(80),
				Status: rand.Intn(2) == 1,
			}

			obj, err := json.Marshal(item)
			if err != nil {
				// For demo purposes, just abort hard.
				log.Fatalf("cannot marshal: %v", err)
			}

			ch <- obj
		}
	}()

	return ch
}
