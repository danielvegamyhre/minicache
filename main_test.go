package main

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/malwaredllc/minicache/client/cache_client"
	"github.com/malwaredllc/minicache/server"
)

const (
	RELATIVE_CONFIG_PATH      = "configs/nodes-local.json"
	RELATIVE_CONFIG_PATH_HTTP = "configs/nodes-local-insecure.json"
	RELATIVE_CLIENT_CERT_DIR  = "certs"
)

// 10 goroutines make 10k requests each via REST API. Count cache misses.
func Test10kConcurrentRestApiPuts(t *testing.T) {
	// start servers
	capacity := 100
	verbose := false
	insecure := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)
	shutdown_chan := make(chan bool, 1)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose, insecure)

	// start client
	c := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path, insecure)
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
	t.Logf("Time to complete 10k puts via REST: %s", elapsed)
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
	shutdown_chan <- true
}

// 10 goroutines make 10k requests each via REST API (but http). Count cache misses.
func Test10kConcurrentRestApiPutsInsecure(t *testing.T) {
	// start servers
	capacity := 100
	verbose := false
	insecure := true
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH_HTTP)
	shutdown_chan := make(chan bool, 1)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose, insecure)

	// start client
	cl := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path, insecure)
	cl.StartClusterConfigWatcher(shutdown_chan)

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
				err := cl.Put(v, v)
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
	t.Logf("Time to complete 10k puts via REST: %s", elapsed)
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

	shutdown_chan <- true
}

// 10 goroutines make 10k requests each vi gRPC. Count cache misses.
func Test10kConcurrentGrpcPuts(t *testing.T) {
	// start servers
	capacity := 100
	verbose := false
	insecure := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)
	shutdown_chan := make(chan bool, 1)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose, insecure)

	// start client
	c := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path, insecure)
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
	t.Logf("Time to complete 10k puts via gRPC API: %s", elapsed)
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

	shutdown_chan <- true
}
