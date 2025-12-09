package main

import (
	"fmt"
	"time"
)

func main() {
	some := make(chan struct{})

	go func() {
		for {
			select {
			case <-some:
				fmt.Println("some")
			}
		}

	}()
	
	time.Sleep(1 * time.Second)
	close(some)
	time.Sleep(100 * time.Millisecond)

}
