// The node package contains data structures useful for representing information about nodes in a distributed system,
// as well as some utility functions for getting information about them.
package node

import (
	"io/ioutil"
	"encoding/json"
	"os"
	"math/rand"
	"time"
)

// NodesConfig struct holds info about all server nodes in the network
type NodesConfig struct {
	Nodes 	[]Node 	`json:"nodes"`
}

// Node struct contains all info we need about a server node, as well as 
// a gRPC client to interact with it
type Node struct {
	Id 					int32 	`json:"id"`
	Host 				string 	`json:"host"`
	Port 				int32 	`json:"port"`
}


// Load nodes config file
func LoadNodesConfig(config_file string) NodesConfig {
	file, _ := ioutil.ReadFile(config_file)
	nodes_config := NodesConfig{}
	_ = json.Unmarshal([]byte(file), &nodes_config)
	return nodes_config
}

// Determine which node ID we are
func GetCurrentNodeId(config NodesConfig) int32 {
	host, _ := os.Hostname()
	for _, node := range config.Nodes {
		if node.Host == host {
			return node.Id
		}
	}
	// if host not found, return -1
	return -1
}

// Get random node from config
func GetRandomNode(config NodesConfig) Node {
	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(config.Nodes))
	return config.Nodes[randomIndex]
}