package main

import (
	"context"
	"testing"
	"strconv"
	"sync"
	"time"
	"path/filepath"
	"github.com/malwaredllc/minicache/server"
	"github.com/malwaredllc/minicache/client/cache_client"
)

const (
	RELATIVE_CONFIG_PATH = "configs/nodes-local.json"
	RELATIVE_CLIENT_CERT_DIR = "certs"
)


// 10 goroutines make 10k requests each via REST API. Count cache misses.
func Test10kConcurrentRestApiPuts(t *testing.T) {
	// start servers
	capacity := 100
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose)

	// start client
	c := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path)
	c.StartClusterConfigWatcher()

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
	t.Logf("Time to complete 10k puts via gRPC: %s", elapsed)
	t.Logf("Cache misses: %d/10,000 (%f%%)", int(miss), miss/10000)

	// cleanup
	for _, srv_comps := range components {
		srv_comps.GrpcServer.Stop()

	    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	    defer cancel()

	    if err := srv_comps.HttpServer.Shutdown(ctx); err != nil {
	        t.Logf("Http server shutdown error: %s", err)
	    }
	}
}


// 10 goroutines make 10k requests each vi gRPC. Count cache misses.
func Test10kConcurrentGrpcPuts(t *testing.T) {
	// start servers
	capacity := 100
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose)

	// start client
	c := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path)
	c.StartClusterConfigWatcher()


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
	t.Logf("Time to complete 10k puts via REST API: %s", elapsed)
	t.Logf("Cache misses: %d/10,000 (%f%%)", int(miss), miss/10000)

	// cleanup
	for _, srv_comps := range components {
		srv_comps.GrpcServer.Stop()

	    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	    defer cancel()

	    if err := srv_comps.HttpServer.Shutdown(ctx); err != nil {
	        t.Logf("Http server shutdown error: %s", err)
	    }
	}
}