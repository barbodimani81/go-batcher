package generator

import (
	"math/rand/v2"
)

type Item struct {
	ID     int  `json:"id"`
	Age    int  `json:"age"`
	Status bool `json:"status"`
}

func ItemGenerator(n int) <-chan Item {
	ch := make(chan Item)

	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			item := Item{
				ID:     rand.IntN(1000000),
				Age:    rand.IntN(80),
				Status: rand.IntN(2) == 1,
			}
			ch <- item
		}
	}()
	return ch
}
