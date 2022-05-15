# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

Distributed cache which supports:
- client-side consistent hashing
- arbitrary cluster sizes
- distributed leader election
- both HTTP/gRPC interfaces for gets/puts
- mTLS secured communication (gRPC only for now, adding to HTTP/REST API soon)

## Contents 

1. [Features](https://github.com/malwaredllc/minicache#features)
2. [Testing](https://github.com/malwaredllc/minicache#testing)
3. [Usage/Examples](https://github.com/malwaredllc/minicache#usage-example-run-distributed-cache-using-docker-containers)

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

### Performance
- 10,000 items stored in cache via gRPC calls in 0.588 seconds when running 4 cache servers on localhost with capacity of 100 items each, when all servers stay online throughout the test:

```
$ go test -v main_test.go
...
    main_test.go:114: Time to complete 10k puts via gRPC: 588.774872ms
    main_test.go:115: Cache misses: 0/10,000 (0.000000%)
```

Test environment:
- **2013 MacBook Pro**
- **Processor**: 2.4 GHz Intel Core i5
- **Memory**: 8 GB 1600 MHz DDR3 


- REST API is much slower at ~18 seconds (TODO: why???)

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

**PRO TIP**: a useful test is to to manually stop/restart arbitrary nodes in the cluster and observe the test log output to see the consistent hashing ring update in real time.

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


