# minicache

![build badge](https://github.com/malwaredllc/minicache/actions/workflows/go.yml/badge.svg)

This may not be the best distributed cache, but it is a distributed cache.

Features:
- LRU (least-recently-used) eviction policy
- Get/Put operations run in O(1) time
- Eviction runs in O(1) time
- Client uses consistent hashing to uniformly distribute requests and minimize required re-mappings when servers join/leave the cluster
- Fault tolerance handled via single-leader replication
- Bully election algorithm 

