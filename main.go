package main

import (
	"flag"
	"fmt"
	"github.com/malwaredllc/minicache/server"
	"log"
	"net"
)

func main() {
	// parse arguments
	port := flag.Int("port", 8080, "port number for server to listen on")
	capacity := flag.Int("capacity", 2, "capacity of LRU cache")
	verbose := flag.Bool("verbose", false, "log events to terminal")
	config_file := flag.String("config", "", "filename of JSON config file with node info")
	flag.Parse()

	// set up listener TCP connectiion
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		panic(err)
	}

	// get new grpc id server
	grpc_server, cache_server := server.GetNewCacheServer(*capacity, *config_file, *verbose)

	// run gRPC server
	log.Printf("Running gRPC server on port %d...", 5005)
	go grpc_server.Serve(listener)

	// run initial election
	cache_server.RunElection()

	// start leader heartbeat monitor
	go cache_server.StartLeaderHeartbeatMonitor()

	// run HTTP server
	cache_server.RunHttpServer(*port)
}