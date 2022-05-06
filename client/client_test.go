package client

import (
	"testing"
	"strconv"
	"sync"
)

// 10 goroutines make 1k requests each
func Benchmark10kConcurrentPuts(b *testing.B) {
	c := NewClientWrapper("")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		go func(wg *sync.WaitGroup) {
			wg.Add(1)
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				c.Put(v, v)
			}
			wg.Done()
		}(&wg)
	}	
	wg.Wait()
}