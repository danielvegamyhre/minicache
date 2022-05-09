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
	grpc_port := flag.Int("grpc-port", 5005, "port number for gRPC server to listen on")
	capacity := flag.Int("capacity", 2, "capacity of LRU cache")
	verbose := flag.Bool("verbose", false, "log events to terminal")
	config_file := flag.String("config", "", "filename of JSON config file with node info")
	rest_port := flag.Int("rest-port", 8080, "enable REST API for client requests, instead of just gRPC")

	flag.Parse()

	// set up listener TCP connectiion
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *grpc_port))
	if err != nil {
		panic(err)
	}

	// get new grpc id server
	grpc_server, cache_server := server.GetNewCacheServer(*capacity, *config_file, *verbose)

	// run gRPC server
	log.Printf("Running gRPC server on port %d...", *grpc_port)
	go grpc_server.Serve(listener)

	// run initial election
	cache_server.RunElection()

	// start leader heartbeat monitor
	go cache_server.StartLeaderHeartbeatMonitor()

	// run HTTP server
	log.Printf("Running REST API server on port %d...", *rest_port)
	cache_server.RunHttpServer(*rest_port)
}
