package cache_client

import (
	"fmt"
	"bytes"
	"log"
	"net/http"
	"encoding/json"
	"io/ioutil"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/ring"
)

// Client wrapper for interacting with Identity Server
type ClientWrapper struct {
	Config 		node.NodesConfig
	Ring		*ring.Ring
}

type Payload struct {
	Key 	string		`json:"key"`
	Value	string		`json:"value"`
}

// Create new Client struct instance and sets up node ring with consistent hashing
func NewClientWrapper(config_file string) *ClientWrapper {
	// get nodes config
	nodes_config := node.LoadNodesConfig(config_file)
	ring := ring.NewRing()
	for _, node := range nodes_config.Nodes {
		ring.AddNode(node.Id, node.Host, node.Port)
	}
	return &ClientWrapper{Config: nodes_config, Ring: ring}
}

// Get item from cache. Hash the key to find which node has the value stored.
func (c *ClientWrapper) Get(key string) string {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make request
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/get", nodeInfo.Host, nodeInfo.Port))

	if err != nil {
        log.Fatal(err)
    }

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)

    if err != nil {
        log.Fatal(err)
    }

    return string(body)
}

// Put key-value pair into cache. Hash the key to find which node to store it in.
func (c *ClientWrapper) Put(key string, value string) string {
	// find node to put the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]


	// encode json payload
	payload := Payload{Key: key, Value: value}
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(payload)

	// make request
	host := fmt.Sprintf("http://%s:%d/put", nodeInfo.Host, nodeInfo.Port)
	req, err := http.NewRequest("POST", host, b)
	if err != nil {
	   log.Fatal(err)
	}

	// check response
	resp, err := new(http.Client).Do(req)
	if err != nil {
		log.Fatal(err)
	}

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        log.Fatal(err)
    }
	return string(body)
}