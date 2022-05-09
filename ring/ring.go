package ring

import (
	"errors"
	"sort"
	"sync"
	"github.com/malwaredllc/minicache/node"
)

var ErrNodeNotFound = errors.New("node not found")

type Ring struct {
	Nodes node.Nodes
	sync.Mutex
}

func NewRing() *Ring {
	return &Ring{Nodes: node.Nodes{}}
}

func (r *Ring) AddNode(group string, id string, host string, rest_port int32, grpc_port int32) {
	r.Lock()
	defer r.Unlock()

	node := node.NewNode(group, id, host, rest_port, grpc_port)
	r.Nodes = append(r.Nodes, node)

	sort.Sort(r.Nodes)
}

func (r *Ring) RemoveNode(id string) error {
	r.Lock()
	defer r.Unlock()

	// improve beyond simple linear scan
	var i int
	for i = 0; i < len(r.Nodes); i++ {
		if r.Nodes[i].Id == id {
			break
		}
	}
	if i == len(r.Nodes) {
		return ErrNodeNotFound
	}

	r.Nodes = append(r.Nodes[:i], r.Nodes[i+1:]...)

	return nil
}

func (r *Ring) Get(id string) string {
	i := r.search(id)
	if i >= r.Nodes.Len() {
		i = 0
	}

	return r.Nodes[i].Id
}

// Binary search in sorted hash id space for nearest node group that should store data for this key.
func (r *Ring) search(key string) int {
	searchfn := func(i int) bool {
		return r.Nodes[i].HashId >= node.HashId(key)
	}

	return sort.Search(r.Nodes.Len(), searchfn)
}
