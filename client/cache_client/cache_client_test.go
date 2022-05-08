package cache_client

import (
	"testing"
	"strconv"
	"sync"
)

// 10 goroutines make 1k requests each
func Test10kConcurrentRestApiPuts(t *testing.T) {
	c := NewClientWrapper("../../configs/nodes-docker.json")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				_ = c.Put(v, v)
			}
		}()
	}	
	wg.Wait()
}

func Test10kConcurrentGrpcPuts(t *testing.T) {
	c := NewClientWrapper("../../configs/nodes-docker.json")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				c.PutGrpc(v, v)
			}
		}()
	}	
	wg.Wait()
}