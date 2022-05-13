package main

import (
	"strconv"
	"sync"
	"github.com/malwaredllc/minicache/client/cache_client"
)

// 10 goroutines make 1k requests each
func main() {
	c := cache_client.NewClientWrapper("../configs/nodes-docker.json")
	c.StartClusterConfigWatcher()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				c.Put(v, v)
			}
		}()
	}	
	wg.Wait()
}