// The node package contains data structures useful for representing information about nodes in a distributed system,
// as well as some utility functions for getting information about them.
package node

import (
	"io/ioutil"
	"encoding/json"
	"os"
	"hash/crc32"
)


// NodesConfig struct holds info about all server nodes in the network
type NodesConfig struct {
	Nodes 	map[string]*Node 	`json:"nodes"`
}

// Node struct contains all info we need about a server node, as well as 
// a gRPC client to interact with it
type Node struct {
	Id 					string  `json:"id"`
	Host 				string 	`json:"host"`
	Port 				int32 	`json:"port"`
	HashId 				uint32
}

func NewNode(id string, host string, port int32) *Node {
	return &Node{
		Id:     id,
		Host:	host,
		Port: 	port,
		HashId: HashId(id),
	}
}

type Nodes []*Node

func (n Nodes) Len() int           { return len(n) }
func (n Nodes) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n Nodes) Less(i, j int) bool { return n[i].HashId < n[j].HashId }


func HashId(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// Load nodes config file
func LoadNodesConfig(config_file string) NodesConfig {
	file, _ := ioutil.ReadFile(config_file)
	nodes_config := NodesConfig{}
	_ = json.Unmarshal([]byte(file), &nodes_config)

	// if config is empty, add 1 node at localhost:8080
	if len(nodes_config.Nodes) == 0 {
		nodes_config = NodesConfig{Nodes: make(map[string]*Node)}
		default_node := NewNode("node0", "localhost", 8080)
		nodes_config.Nodes[default_node.Id] = default_node
	} else {
		for _, nodeInfo := range nodes_config.Nodes {
			nodeInfo.HashId = HashId(nodeInfo.Id)
		}
	}
	return nodes_config
}

// Determine which node ID we are
func GetCurrentNodeId(config NodesConfig) string {
	host, _ := os.Hostname()
	for _, node := range config.Nodes {
		if node.Host == host {
			return node.Id
		}
	}
	// if host not found, default to node 0
	return "node0"
}
