package cache_client

import (
	"testing"
	"strconv"
	"sync"
)

// 10 goroutines make 10k requests each via REST API. Count cache misses.
func Test10kConcurrentRestApiPutsDocker(t *testing.T) {
	c := NewClientWrapper("../../configs/nodes-docker.json")
	c.StartClusterConfigWatcher()

	var wg sync.WaitGroup
	var mutex sync.Mutex
	miss := 0.0

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				err := c.Put(v, v)
				if err != nil {
					mutex.Lock()
					miss += 1
					mutex.Unlock()
				}
			}
		}()
	}	
	wg.Wait()
	t.Logf("Cache misses: %d/100,000 (%f%%)", int(miss), miss/100000)
}


// 10 goroutines make 10k requests each vi gRPC. Count cache misses.
func Test10kConcurrentGrpcPutsDocker(t *testing.T) {
	c := NewClientWrapper("../../configs/nodes-docker.json")
	c.StartClusterConfigWatcher()

	var wg sync.WaitGroup
	var mutex sync.Mutex
	miss := 0.0

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 1000; i++ {
				v := strconv.Itoa(i)
				err := c.PutGrpc(v, v)
				if err != nil {
					mutex.Lock()
					miss += 1
					mutex.Unlock()
				}
			}
		}()
	}	
	wg.Wait()
	t.Logf("Cache misses: %df100,000 (%f%%)", int(miss), miss/100000)
}