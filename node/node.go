// The node package contains data structures useful for representing information about nodes in a distributed system,
// as well as some utility functions for getting information about them.
package node

import (
	"fmt"
	"encoding/json"
	"hash/crc32"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/malwaredllc/minicache/pb"
)

// NodesConfig struct holds info about all server nodes in the network
type NodesConfig struct {
	Nodes            map[string]*Node `json:"nodes"`
	EnableClientAuth bool             `json:"enable_client_auth"`
	EnableHttps      bool             `json:"enable_https"`
	ServerLogfile    string           `json:"server_logfile"`
	ServerErrfile    string           `json:"server_errfile"`
	ClientLogfile    string           `json:"client_logfile"`
	ClientErrfile    string           `json:"client_errfile"`
}

// Node struct contains all info we need about a server node, as well as
// a gRPC client to interact with it
type Node struct {
	Id         string `json:"id"`
	Host       string `json:"host"`
	RestPort   int32  `json:"rest_port"`
	GrpcPort   int32  `json:"grpc_port"`
	HashId     uint32
	GrpcClient pb.CacheServiceClient
}

func (n *Node) SetGrpcClient(c pb.CacheServiceClient) {
	n.GrpcClient = c
}

func NewNode(id string, host string, rest_port int32, grpc_port int32) *Node {
	return &Node{
		Id:       id,
		Host:     host,
		RestPort: rest_port,
		GrpcPort: grpc_port,
		HashId:   HashId(id),
	}
}

type Nodes []*Node

// implementing methods required for sorting
func (n Nodes) Len() int           { return len(n) }
func (n Nodes) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n Nodes) Less(i, j int) bool { return n[i].HashId < n[j].HashId }

func HashId(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// Load nodes config file
func LoadNodesConfig(configFile string) NodesConfig {
	file, _ := ioutil.ReadFile(configFile)

	// set defaults
	nodesConfig := NodesConfig{
		EnableClientAuth: true,
		EnableHttps:      true,
		ServerLogfile:    "minicache.log",
		ServerErrfile:    "minicache.err",
		ClientLogfile:    "minicache-client.log",
		ClientErrfile:    "minicache-client.err",
	}

	_ = json.Unmarshal([]byte(file), &nodesConfig)

	// if config is empty, add 1 node at localhost:8080
	if len(nodesConfig.Nodes) == 0 {
		log.Printf("couldn't find config file or it was empty: %s", configFile)
		log.Println("using default node localhost")
		nodesConfig = NodesConfig{Nodes: make(map[string]*Node)}
		defaultNode := NewNode("node0", "localhost", 8080, 5005)
		nodesConfig.Nodes[defaultNode.Id] = defaultNode
	} else {
		for _, nodeInfo := range nodesConfig.Nodes {
			nodeInfo.HashId = HashId(nodeInfo.Id)
		}
	}
	return nodesConfig
}

// Determine which node ID we are
func GetCurrentNodeId(config NodesConfig) string {
	host, _ := os.Hostname()
	for _, node := range config.Nodes {
		if node.Host == host {
			return node.Id
		}
	}
	// if host not found, generate random node id
	return randSeq(5)
}

// Get random node from a given list of nodes
func GetRandomNode(nodes []*Node) *Node {
	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(nodes))
	return nodes[randomIndex]
}

func randSeq(n int) string {
	rand.Seed(time.Now().UnixNano())
	letters := []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	node_id := fmt.Sprintf("node-%s", string(b))
	return node_id
}
