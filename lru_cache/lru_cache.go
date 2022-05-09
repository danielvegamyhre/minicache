// LRU Cache implementation which uses a hashmap and doubly-linked list 
// to achieve O(1) get/put operations and O(1) eviction.
package lru_cache

import (
	"sync"
	"errors"
)

// doubly-linked list node
type LLNode struct {
    prev    *LLNode
    next    *LLNode
    key     string
    val     string
}

// Lru cache which uses a hashmap and doubly-linked list to acheive O(1) time for get/put/evict operations
type LruCache struct {
    cache       map[string]*LLNode
    head        *LLNode
    tail        *LLNode
    capacity    int
    size        int
    mutex       sync.RWMutex
}

// initialize and return new lru cache of specified capacity
func NewLruCache(capacity int) LruCache {
    lru_cache := LruCache{
        cache:      make(map[string]*LLNode, capacity),
        head:       &LLNode{prev: nil, next: nil, key: "", val: ""},
        tail:       &LLNode{prev: nil, next: nil, key: "", val: ""},
        capacity:   capacity,
        size:       0,
    }
    lru_cache.head.next = lru_cache.tail
    lru_cache.tail.prev = lru_cache.head
    return lru_cache
}


func (lru *LruCache) Get(key string) (string, error) {
    // read lock
    lru.mutex.RLock()
    defer lru.mutex.RUnlock()

    // case 1: key in cache, move to head of list and return
    if node, ok := lru.cache[key]; ok {
        lru.moveNodeToHead(node)
        return node.val, nil
    }
    // case 2: key does not exist
    return "", errors.New("element does not exist in cache")
}


func (lru *LruCache) Put(key string, value string)  {
    // write lock
    lru.mutex.Lock()
    defer lru.mutex.Unlock()

    // case 1: in cache already
    if node, ok := lru.cache[key]; ok {
        // update value and move to head
        node.val = value
        lru.moveNodeToHead(node)
        return
    }
    
    // case 2: create node and add to head
    node := LLNode{prev: lru.head, next: lru.head.next, key: key, val: value}
        
    // map from key to node ptr
    lru.cache[key] = &node
    
    // next node links
    node.next = lru.head.next
    lru.head.next.prev = &node
    
    // head links
    lru.head.next = &node
    node.prev = lru.head

    // increment size
    lru.size += 1
    
    // evict if over capacity
    if lru.size > lru.capacity {
        lru.evict()
        lru.size -= 1
    }
}

func (lru *LruCache) moveNodeToHead(node *LLNode) {
    // remove from middle
    prev := node.prev
    next := node.next
    if prev != nil {
         prev.next = next   
    }
    if next != nil {
        next.prev = prev
    }

    // add to front
    node.next = lru.head.next
    lru.head.next.prev = node
    node.prev = lru.head
    lru.head.next = node
}

func (lru *LruCache) evict() {
    // delete node from tail
    node := lru.tail.prev
    prev := lru.tail.prev.prev
    prev.next = lru.tail
    lru.tail.prev = prev
    
    // delete entry from map
    delete(lru.cache, node.key)
}
