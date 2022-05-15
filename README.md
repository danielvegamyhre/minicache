# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

This may not be the best distributed cache, but it is a distributed cache.

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
5. After each test, displays % of cache misses.

**PRO TIP**: a useful test is to to manually stop/restart arbitrary nodes in the cluster and observe the test log output to see the consistent hashing ring update in real time.

-------------
## Examples

### Example 1: Docker Containers
1. Run `docker-compose build` from the project root directory to build the Docker images
2. Run `docker-compose up` to spin up all of the containers defined in the `docker-compose.yml` file. By default the config file defines 4 cache server instances, 3 of which are the initial nodes defined in the `configs/nodes-docker.json` config file, and 1 of which is an extra server node which dynamically adds itself to the cluster, in order to demonstrate this functionality.
3. Run `docker build -t cacheclient -f Dockerfile.client .` from the project root directory to build a Docker image for the client.
4. Run `docker run --network minicache_default cacheclient` to run the client in a docker container connected t to the docker compose network the servers are running on. By default, the Dockerfile simply builds the client and runs the integration tests described above, although you can change it to do whatever you want.

### Example 2: Localhost

