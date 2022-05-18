# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

Distributed cache implemented in Go. Like Redis but simpler. Features include:
- Client-side [consistent hashing](https://en.wikipedia.org/wiki/Consistent_hashing) to support fault-tolerance by minimizing the number of key re-remappings required in the event of node failure
- Dynamic node discovery enabling arbitrary cluster sizes
- Distributed leader election via [Bully algorithm](https://en.wikipedia.org/wiki/Bully_algorithm) and leader heartbeat monitors which ensure no single-point of failure
- Both HTTP/gRPC interfaces for gets/puts
- Supports [mTLS](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/) secured communication (gRPC only for now, adding to HTTP/REST API soon)
- Dockerfiles to support containerized deployments


<img src="https://github.com/malwaredllc/minicache/blob/main/docs/consistent_hashing_ring.png" width=600>

## Contents 

- [Features](https://github.com/malwaredllc/minicache#features)
	- [Thread-safe LRU cache with O(1) Operations](https://github.com/malwaredllc/minicache#thread-safe-lru-cache-with-o1-operations)
	- [Consistent Hashing and Fault-Tolerance](https://github.com/malwaredllc/minicache#consistent-hashing)
	- [Distributed leader election algorithm](https://github.com/malwaredllc/minicache#distributed-leader-election-algorithm)
	- [No single point of failure](https://github.com/malwaredllc/minicache#no-single-point-of-failure)
	- [Support for both HTTP/gRPC](https://github.com/malwaredllc/minicache#supports-both-rest-api-and-grpc)
	- [Supports mTLS for maximum security](https://github.com/malwaredllc/minicache#mtls-for-maximum-security)
- [Performance/Benchmarking](https://github.com/malwaredllc/minicache#performance)
	- [LRU Cache direct usage performance](https://github.com/malwaredllc/minicache#1-lru-cache-implemtation-ran-directly-by-a-test-program)
	- [Local distributed cache performance](https://github.com/malwaredllc/minicache#2-distributed-cache-running-locally-with-storage-via-grpc-calls-over-local-network)
	- [Distributed cache ran in Docker containers](https://github.com/malwaredllc/minicache#3-distributed-cache-storage-running-in-docker-containers-with-storage-via-grpc-calls)

- [Testing](https://github.com/malwaredllc/minicache#testing)
	- [Unit testing](https://github.com/malwaredllc/minicache#1-unit-tests)
	- [Integration testing](https://github.com/malwaredllc/minicache#2-integration-tests)
	- [Consistent Hashing and Fault-tolerance testing](https://github.com/malwaredllc/minicache/blob/main/lru_cache/lru_cache_test.go)
- [Set Up and Usage](https://github.com/malwaredllc/minicache#set-up-and-usage)
	- [Enabling/Disabling TLS](https://github.com/malwaredllc/minicache#set-up-and-usage)
- [Examples](https://github.com/malwaredllc/minicache#examples)
	- [Running a distributed cache with Docker Compose](https://github.com/malwaredllc/minicache#example-1-run-distributed-cache-using-docker-containers)
	- [Running all cache servers defined in a config file](https://github.com/malwaredllc/minicache#example-2-starting-all-cache-servers-defined-in-config-file)
	- [Running a single cache server](https://github.com/malwaredllc/minicache#example-3-starting-a-single-cache-server)
	- [Creating and using a cache client](https://github.com/malwaredllc/minicache#example-4-creating-and-using-a-cache-client)
- [Contributing](https://github.com/malwaredllc/minicache#contributing)

----------

## Features

### Thread-safe LRU cache with O(1) operations
- Least-recently-used eviction policy with a configurable cache capacity ensures low cache-miss rate
- Get/Put operations and eviction run all run in **O(1) time**
- LRU cache implementation is made thread safe by use of Go synchronization primitives

### Consistent Hashing
- Client uses [consistent hashing](https://en.wikipedia.org/wiki/Consistent_hashing) to uniformly distribute requests and minimize required re-mappings when servers join/leave the cluster
- Client automatically monitors the cluster state stored on the leader node for any changes and updates its consistent hashing ring accordingly

### Distributed leader election algorithm
- [Bully election algorithm](https://en.wikipedia.org/wiki/Bully_algorithm) used to elect a leader node for the cluster, which is in charge of monitoring the state of the nodes in the cluster to provide to clients so they can maintain a consistent hashing ring and route requests to the correct nodes
- Follower nodes monitor heartbeat of leader and run a new election if it goes down

### Dynamic node discovery
- New nodes join the cluster when they come online by first registering themselves with the cluster, which is done by sending identifying information (hostname, port, etc.) to each of the cluster's original "genesis" nodes (i.e. nodes defined in the config file used at runtime) until one returns a successful response. 
- When an existing node receives this registration request from the new node, it will add the new node to its in-memory list of nodes and send this updated list to all other nodes.
- The leader node monitors heartbeats of all nodes in the cluster, keeping a list of active reachable nodes in the cluster updated in real-time.
- Clients monitor the leader's cluster config for changes and updates their consistent hashing ring accordingly. Time to update cluster state after node joins/leaves cluster is <= 1 second.

### No single point of failure
- The distributed election algorithm allows any nodes to arbitrarily join/leave cluster at any time, and there is always guaranteed to be a leader tracking the state of nodes in the cluster to provide to clients for consistent hashing.

### Supports both REST API and gRPC
- Make gets/puts with the simple familiar interfaces of HTTP/gRPC 

### mTLS for maximum security
- minicache uses [mutual TLS](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/), with mutual authentication between client and server for maximum security.

---------------

## Performance

### Test environment:
- **2013 MacBook Pro**
- **Processor**: 2.4 GHz Intel Core i5
- **Memory**: 8 GB 1600 MHz DDR3 

### 1. LRU Cache implementation ran directly by a test program: 

**Test**: 10 million puts calling a LRU cache with capacity of 10,000 directly in memory:

```
$ go test -v ./lru_cache

=== RUN   TestCacheWriteThroughput
    lru_cache_test.go:19: Time to complete 10M puts: 3.869112083s
    lru_cache_test.go:20: LRU Cache write throughput: 2584572.321887 puts/second

```
**Result**: 2.58 million puts/second

### 2. Distributed cache running locally with storage via gRPC calls over local network

**Test**: 10,000 items stored in cache via gRPC calls when running 4 cache servers on localhost with capacity of 100 items each, when all servers stay online throughout the test

```
$ go test -v main_test.go
	...
    main_test.go:114: Time to complete 10k puts via gRPC: 588.774872ms
```

**Result**: ~17,000 puts/second

### 3. Distributed cache storage running in Docker containers with storage via gRPC calls: 


**Test**: 10,000 items stored in cache via gRPC calls when running 4 cache servers on localhost with capacity of 100 items each, when all servers stay online throughout the test

```
# docker run --network minicache_default cacheclient
	...
	cache_client_docker_test.go:95: Time to complete 10k puts via REST API: 8.6985474s
``` 

**Result**: 1150 puts/second

------------

## Testing

### 1. Unit tests
- LRU Cache implementation has exhaustive unit tests for correctness in all possible scenarios (see [lru_cache_test.go](https://github.com/malwaredllc/minicache/blob/main/lru_cache/lru_cache_test.go))
- Run the unit tests with the command `go test -v ./lru_cache`
- Consistent hashing ring contains exhaustive unit tests for various scenarios (see [ring_test.go](https://github.com/malwaredllc/minicache/blob/main/ring/ring_test.go))
- Run these unit tests with the command `go test -v ./ring`

### 2. Integration tests
Run the integration tests with the command `go test -v main_test.go`, which performs the following steps:

1. Spins up multiple cache server instances locally on different ports (see [nodes-local.json](https://github.com/malwaredllc/minicache/blob/main/configs/nodes-local.json) config file)
2. Creates cache client
3. Runs 10 goroutines which each send 1000 requests to put items in the distributed cache via REST API endpoint
4. Runs 10 goroutines which each send 1000 requests to put items in the distributed cache via gRPC calls
5. After each test, displays % of cache misses (which in this case, is when the client is simply unable to store an item in the distributed cache)

### 3. Fault-tolerance testing

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

## Set Up and Usage

### 1. Create/update node configuration file

You will need to define 1 or more initial "genesis" nodes in a JSON config file (see [nodes-local.json](https://github.com/malwaredllc/minicache/blob/main/configs/nodes-local.json) or [nodes-docker.json](https://github.com/malwaredllc/minicache/blob/main/configs/nodes-docker.json) for working examples). 

These genesis nodes are the original nodes of the cluster, which any new nodes created later on will attempt to contact in order to dynamically register themselves with the cluster. As long as at least 1 of these initial nodes is online, any arbitrary number of new nodes can be spun up (e.g. launching more cache server containers from an image) without defining them in a config file, rebuilding the image etc. 

Therefore it is recommended to define at least 3 initial nodes to support fault-tolerance to a reasonable level.

### 2. Enabling/Disabling TLS

In the node configuration file:

- **To disable TLS** set `enable_https: false`
- **To enable TLS** set `enable_https: true`
- **To enable mTLS** set `enable_https: true` and `enable_client_auth: true`


### 3. Generating TLS certificates

If you want to enable mTLS, you will need to generate TLS certificates by performing the following steps:

- Update config files `certs/client-ext.cnf` and `certs/server-ext.cnf` to include any hostnames and/or IPs of any servers you plan on running. If you plan on using Docker containers, the DNS hostnames should match those of the docker containers defined in `docker-compose.yml`. By default, the following hostnames and IPs are defined in the config files (note the hostnames match those defined in `docker-compose.yml`):

```
subjectAltName = DNS:localhost,DNS:cacheserver0,DNS:cacheserver1,DNS:cacheserver2,DNS:cacheserver3,IP:0.0.0.0,IP:127.0.0.1
```

- Run `./gen.sh` which will generate the TLS certificates and store them in the appropriate location (`/certs`). 

### 3. Run cache servers and clients by following any of the examples below

--------------
## Examples

### Example 1: Run Distributed Cache Using Docker Containers

1. Run `docker-compose build` from the project root directory to build the Docker images.

2. Run `docker-compose up` to spin up all of the containers defined in the `docker-compose.yml` file. By default the config file defines 4 cache server instances, 3 of which are the initial nodes defined in the `configs/nodes-docker.json` config file, and 1 of which is an extra server node which dynamically adds itself to the cluster, in order to demonstrate this functionality.

3. Run `docker build -t cacheclient -f Dockerfile.client .` from the project root directory to build a Docker image for the client.

4. Run `docker run --network minicache_default cacheclient` to run the client in a docker container connected t to the docker compose network the servers are running on. By default, the Dockerfile simply builds the client and runs the integration tests described above, although you can change it to do whatever you want.

**PRO TIP**: a useful test is to to manually stop/restart arbitrary nodes in the cluster and observe the test log output to see the consistent hashing ring update in real time.

### Example 2: Starting All Cache Servers Defined in Config File

In the example below:
- `RELATIVE_CERT_DIR` is the directory containing TLS certificates you generated [here](https://github.com/malwaredllc/minicache#set-up-and-usage)
- `RELATIVE_CONFIG_PATH` is the path to the JSON config file containing the initial node information, discussed [here](https://github.com/malwaredllc/minicache#set-up-and-usage)

```go
	// start servers
	capacity := 100
	verbose := false
	abs_cert_dir, _ := filepath.Abs(RELATIVE_CERT_DIR)
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

### Example 3: Starting a Single Cache Server

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

### Example 4: Creating and Using a Cache Client

In the example below:
- `abs_cert_dir` is the directory containing TLS certificates you generated [here](https://github.com/malwaredllc/minicache#set-up-and-usage)
- `abs_config_path` is the path to the JSON config file containing the initial node information, discussed [here](https://github.com/malwaredllc/minicache#set-up-and-usage)

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

## Contributing
Feel free to take a look at some of the issues and feature requests [here](https://github.com/malwaredllc/minicache/issues) and submit a pull-request.


- Make using TLS optional
- TLS support for REST API
- Move to Raft election algorithm instead of Bully algorithm
- Improve performance of REST API
- Improve code quality and project organization
- Documentation
