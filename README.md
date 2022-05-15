# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

This may not be the best distributed cache, but it is a distributed cache.

### Thread-safe LRU cache with O(1) operations
- Least-recently-used eviction policy with a configurable cache capacity ensures low cache-miss rate
- Get/Put operations and eviction run all run in **O(1) time**
- LRU cache implementation is thread safe

### Consistent Hashing
- Client uses **consistent hashing** to uniformly distribute requests and minimize required re-mappings when servers join/leave the cluster
- **Bully election algorithm** used to elect a leader node for the cluster
- Follower nodes monitor heartbeat of leader and run a new election if it goes down

### Dynamic cluster state, nodes can arbitrarily join/leave cluster
- Leader node monitors heartbeats of all nodes in the cluster, keeping a list of active reachable nodes in the cluster updated in real-time
- Client monitors the leader's cluster config for changes and updates its consistent hashing ring accordingly. Time to update cluster state after node joins/leaves cluster is <= 1 second.

### No single point of failure
- The distributed election algorithm allows any nodes to arbitrarily join/leave cluster at any time, and there is always guaranteed to be a leader tracking the state of nodes in the cluster to provide to clients for consistent hashing.

------------

## Example Usage 
