package cache

// doubly-linked list node
type Node struct {
    prev    *Node
    next    *Node
    key     int
    val     int
}

// LRU cache which uses a hashmap and doubly-linked list to acheive O(1) time for get/put/evict operations
type LRUCache struct {
    cache       map[int]*Node
    head        *Node
    tail        *Node
    capacity    int
    size        int
}

// initialize and return new lru cache of specified capacity
func New(capacity int) LRUCache {
    lru_cache := LRUCache{
        cache:      make(map[int]*Node, capacity),
        head:       &Node{prev: nil, next: nil, key: -1, val: -1},
        tail:       &Node{prev: nil, next: nil, key: -1, val: -1},
        capacity:   capacity,
        size:       0,
    }
    lru_cache.head.next = lru_cache.tail
    lru_cache.tail.prev = lru_cache.head
    return lru_cache
}


func (lru *LRUCache) Get(key int) int {
    // case 1: key in cache, move to head of list and return
    if node, ok := lru.cache[key]; ok {
        lru.moveNodeToHead(node)
        return node.val
    }
    // case 2: key does not exist
    return -1
}


func (lru *LRUCache) Put(key int, value int)  {
    // case 1: in cache already
    if node, ok := lru.cache[key]; ok {
        // update value and move to head
        node.val = value
        lru.moveNodeToHead(node)
        return
    }
    
    // case 2: create node and add to head
    node := Node{prev: lru.head, next: lru.head.next, key: key, val: value}
        
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

func (lru *LRUCache) moveNodeToHead(node *Node) {
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

func (lru *LRUCache) evict() {
    // delete node from tail
    node := lru.tail.prev
    prev := lru.tail.prev.prev
    prev.next = lru.tail
    lru.tail.prev = prev
    
    // delete entry from map
    delete(lru.cache, node.key)
}
