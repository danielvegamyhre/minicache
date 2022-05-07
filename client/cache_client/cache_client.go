package cache_client

import (
	"fmt"
	"bytes"
	"log"
	"net/http"
	"encoding/json"
	"io/ioutil"
	"crypto/tls"
	"crypto/x509"
	"context"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/ring"
	"github.com/malwaredllc/minicache/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
		
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

// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func NewCacheClient(server_host string, server_port int) pb.CacheServiceClient {
	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		log.Fatalf("failed to create credentials: %v", err)
	}

	// set up connection
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", server_host, server_port), grpc.WithTransportCredentials(creds))
	if err != nil {
		panic(err)
	}

	// set up client
	return pb.NewCacheServiceClient(conn)
}

// Utility function to set up mTLS config and credentials
func LoadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile("certs/ca-cert.pem")
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair("certs/client-cert.pem", "certs/client-key.pem")
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}