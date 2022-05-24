package client

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	RELATIVE_CONFIG_PATH     = "../configs/nodes-docker-with-mTLS.json"
	RELATIVE_CLIENT_CERT_DIR = "../certs"
)

// 10 goroutines make 10k requests each via REST API. Count cache misses.
func Test10kRestApiPuts(t *testing.T) {
	// set up parameters for client
	insecure := false
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)
	shutdown_chan := make(chan bool, 1)

	// start client
	c := NewClientWrapper(abs_cert_dir, abs_config_path, insecure, verbose)
	c.StartClusterConfigWatcher(shutdown_chan)

	var wg sync.WaitGroup
	var mutex sync.Mutex
	miss := 0.0

	// start timer
	start := time.Now()

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
	elapsed := time.Since(start)

	shutdown_chan <- true

	t.Logf("Time to complete 10k puts via gRPC: %s", elapsed)
	t.Logf("Cache misses: %d/10,000 (%f%%)", int(miss), miss/10000)
}

// 10 goroutines make 10k requests each vi gRPC. Count cache misses.
func Test10kGrpcPuts(t *testing.T) {
	// set up parameters for client
	insecure := false
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)
	shutdown_chan := make(chan bool, 1)

	// start client
	c := NewClientWrapper(abs_cert_dir, abs_config_path, insecure, verbose)
	c.StartClusterConfigWatcher(shutdown_chan)

	var wg sync.WaitGroup
	var mutex sync.Mutex
	miss := 0.0

	// start timer
	start := time.Now()
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
	elapsed := time.Since(start)

	shutdown_chan <- true

	t.Logf("Time to complete 10k puts via REST API: %s", elapsed)
	t.Logf("Cache misses: %d/10,000 (%f%%)", int(miss), miss/10000)
}
