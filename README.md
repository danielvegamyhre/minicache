# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

Distributed cache which supports:
- client-side consistent hashing to support fault-tolerance by minimizing the number of key re-remappings required in the event of node failure
- arbitrary cluster sizes
- distributed leader election algorithm and leader heartbeat monitor which ensure no single-point of failure
- both HTTP/gRPC interfaces for gets/puts
- mTLS secured communication (gRPC only for now, adding to HTTP/REST API soon)
- Dockerfiles to support containerized deployments

## Contents 

1. [Features](https://github.com/malwaredllc/minicache#features)
2. [Performance](https://github.com/malwaredllc/minicache#performance)
3. [Testing](https://github.com/malwaredllc/minicache#testing)
4. [Usage/Examples](https://github.com/malwaredllc/minicache#usage-example-run-distributed-cache-using-docker-containers)

<img src="https://github.com/malwaredllc/minicache/blob/main/docs/consistent_hashing_ring.png" width=600>

----------

## Features

### Thread-safe LRU cache with O(1) operations
- Least-recently-used eviction policy with a configurable cache capacity ensures low cache-miss rate
- Get/Put operations and eviction run all run in **O(1) time**
- LRU cache implementation is thread safe

### Consistent Hashing
- Client uses [consistent hashing](https://en.wikipedia.org/wiki/Consistent_hashing) to uniformly distribute requests and minimize required re-mappings when servers join/leave the cluster
- Client automatically monitors the cluster state stored on the leader node for any changes and updates its consistent hashing ring accordingly

### Distributed leader election algorithm
- [Bully election algorithm](https://en.wikipedia.org/wiki/Bully_algorithm) used to elect a leader node for the cluster, which is in charge of monitoring the state of the nodes in the cluster to provide to clients so they can maintain a consistent hashing ring and route requests to the correct nodes
- Follower nodes monitor heartbeat of leader and run a new election if it goes down

### Dynamic cluster state, nodes can arbitrarily join/leave cluster
- Leader node monitors heartbeats of all nodes in the cluster, keeping a list of active reachable nodes in the cluster updated in real-time
- Client monitors the leader's cluster config for changes and updates its consistent hashing ring accordingly. Time to update cluster state after node joins/leaves cluster is <= 1 second.

### No single point of failure
- The distributed election algorithm allows any nodes to arbitrarily join/leave cluster at any time, and there is always guaranteed to be a leader tracking the state of nodes in the cluster to provide to clients for consistent hashing.

### Supports both REST API and gRPC
- Make gets/puts with the simple familiar interfaces of HTTP/gRPC 

### mTLS for maximum security
- minicache uses [mutual TLS](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/), with mutual authentication between client and server for maximum security.

---------------

## Performance
1. **LRU Cache implemtation ran directly by a test program**: ~4.17 million puts/second

2. **Distributed cache running locally with storage via gRPC calls over local network**: ~17,000 puts/second (10,000 items stored in cache via gRPC calls in 0.588 seconds when running 4 cache servers on localhost with capacity of 100 items each, when all servers stay online throughout the test):

```
$ go test -v main_test.go
	...
    main_test.go:114: Time to complete 10k puts via gRPC: 588.774872ms
    main_test.go:115: Cache misses: 0/10,000 (0.000000%)
```

3. **Distributed cache storage running in Docker containers with storage via gRPC calls**: 1150 puts/second (10,000 items stored in cache via gRPC calls in 8.69 seconds when running 4 cache servers on localhost with capacity of 100 items each, when all servers stay online throughout the test)

```
# docker run --network minicache_default cacheclient
	...
	cache_client_docker_test.go:95: Time to complete 10k puts via REST API: 8.6985474s
``` 

### Test environment:
- **2013 MacBook Pro**
- **Processor**: 2.4 GHz Intel Core i5
- **Memory**: 8 GB 1600 MHz DDR3 

------------

## Testing

### Unit tests
- LRU Cache implementation has exhaustive unit tests for correctness in all possible scenarios (see [lru_cache_test.go](https://github.com/malwaredllc/minicache/blob/main/lru_cache/lru_cache_test.go))
- Run the unit tests with the command `go test -v ./lru_cache`

### Integration tests
Run the integration tests with the command `go test -v main_test.go`, which performs the following steps:

1. Spins up multiple cache server instances locally on different ports (see [nodes-local.json](https://github.com/malwaredllc/minicache/blob/main/configs/nodes-local.json) config file)
2. Creates cache client
3. Runs 10 goroutines which each send 1000 requests to put items in the distributed cache via REST API endpoint
4. Runs 10 goroutines which each send 1000 requests to put items in the distributed cache via gRPC calls
5. After each test, displays % of cache misses (which in this case, is when the client is simply unable to store an item in the distributed cache)

### Fault-tolerance testing

A useful test is to to manually stop/restart arbitrary nodes in the cluster and observe the test log output to see the consistent hashing ring update in real time. 


Example of stopping and restarting cacheserver1 while integration tests are running:

```
2022/05/15 02:23:35 cluster config: [id:"node2" host:"cacheserver2" rest_port:8080 grpc_port:5005 id:"epQKE" host:"cacheserver3" rest_port:8080 grpc_port:5005 id:"node0" host:"cacheserver0" rest_port:8080 grpc_port:5005]
...
2022/05/15 02:23:35 Removing node node1 from ring
...
2022/05/15 02:23:36 cluster config: [id:"epQKE" host:"cacheserver3" rest_port:8080 grpc_port:5005 id:"node0" host:"cacheserver0" rest_port:8080 grpc_port:5005 id:"node2" host:"cacheserver2" rest_port:8080 grpc_port:5005]
...

2022/05/15 02:23:40 Adding node node1 to ring
...
2022/05/15 02:23:41 cluster config: [id:"node2" host:"cacheserver2" rest_port:8080 grpc_port:5005 id:"epQKE" host:"cacheserver3" rest_port:8080 grpc_port:5005 id:"node0" host:"cacheserver0" rest_port:8080 grpc_port:5005 id:"node1" host:"cacheserver1" rest_port:8080 grpc_port:5005]
```

-------------

## Usage Example: Run Distributed Cache Using Docker Containers

1. **Generate TLS certificates**: update config files `certs/client-ext.cnf` and `certs/server-ext.cnf` to include any hostnames and/or IPs of any servers you plan on running, then run `./gen.sh` which will generate the TLS certificates and store them in the appropriate location (`/certs`). If you plan on using Docker containers, the DNS hostnames should match those of the docker containers defined in `docker-compose.yml`.

By default, the following hostnames and IPs are defined in the config files:

```
subjectAltName = DNS:localhost,DNS:cacheserver0,DNS:cacheserver1,DNS:cacheserver2,DNS:cacheserver3,IP:0.0.0.0,IP:127.0.0.1
```

2. Run `docker-compose build` from the project root directory to build the Docker images.

3. Run `docker-compose up` to spin up all of the containers defined in the `docker-compose.yml` file. By default the config file defines 4 cache server instances, 3 of which are the initial nodes defined in the `configs/nodes-docker.json` config file, and 1 of which is an extra server node which dynamically adds itself to the cluster, in order to demonstrate this functionality.

4. Run `docker build -t cacheclient -f Dockerfile.client .` from the project root directory to build a Docker image for the client.

5. Run `docker run --network minicache_default cacheclient` to run the client in a docker container connected t to the docker compose network the servers are running on. By default, the Dockerfile simply builds the client and runs the integration tests described above, although you can change it to do whatever you want.

**PRO TIP**: a useful test is to to manually stop/restart arbitrary nodes in the cluster and observe the test log output to see the consistent hashing ring update in real time.

## Usage Example 2: Starting a Single Cache Server

```go
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
	grpc_server, cache_server := server.NewCacheServer(*capacity, *config_file, *verbose, server.DYNAMIC)

	// run gRPC server
	log.Printf("Running gRPC server on port %d...", *grpc_port)
	go grpc_server.Serve(listener)

	// register node with cluster
	cache_server.RegisterNodeInternal()

	// run initial election
	cache_server.RunElection()

	// start leader heartbeat monitor
	go cache_server.StartLeaderHeartbeatMonitor()

	// run HTTP server
	log.Printf("Running REST API server on port %d...", *rest_port)
	http_server := cache_server.RunAndReturnHttpServer(*rest_port)

	// set up shutdown handler and block until sigint or sigterm received
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-c

		log.Printf("Shutting down gRPC server...")
		grpc_server.Stop()


		log.Printf("Shutting down HTTP server...")
	    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	    defer cancel()

	    if err := http_server.Shutdown(ctx); err != nil {
	        log.Printf("Http server shutdown error: %s", err)
	    }
		os.Exit(0)
	}()

	// block indefinitely
	select {}
}
```

## Usage Example 3: Starting All Cache Servers Defined in Config File

```go
	// start servers
	capacity := 100
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CLIENT_CERT_DIR)
	abs_config_path, _ := filepath.Abs(RELATIVE_CONFIG_PATH)

	components := server.CreateAndRunAllFromConfig(capacity, abs_config_path, verbose)
	
	...

	// cleanup
	for _, srv_comps := range components {
		srv_comps.GrpcServer.Stop()

	    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	    defer cancel()

	    if err := srv_comps.HttpServer.Shutdown(ctx); err != nil {
	        t.Logf("Http server shutdown error: %s", err)
	    }
	}
```

## Usage Example 4: Creating and Using a Cache Client

```go
	// start client
	c := cache_client.NewClientWrapper(abs_cert_dir, abs_config_path)
	c.StartClusterConfigWatcher()
	...
	c.Put(key, value)
	...
	val := c.Get(key)
```

---------------

## To Do
- TLS for REST API
- Improve performance of REST API
- Improve code quality and project organization
- Documentation
