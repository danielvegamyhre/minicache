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
	"time"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/ring"
	"github.com/malwaredllc/minicache/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"	
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
		c := NewCacheClient(node.Host, int(node.GrpcPort))
		node.SetGrpcClient(c)
		ring.AddNode(node.Group, node.Id, node.Host, node.RestPort, node.GrpcPort)
	}
	return &ClientWrapper{Config: nodes_config, Ring: ring}
}

// Get item from cache. Hash the key to find which node has the value stored.
func (c *ClientWrapper) Get(key string) string {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make request
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/get", nodeInfo.Host, nodeInfo.RestPort))

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
	host := fmt.Sprintf("http://%s:%d/put", nodeInfo.Host, nodeInfo.RestPort)
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

func (c *ClientWrapper) GetGrpc(key string) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	res, err := nodeInfo.GrpcClient.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		log.Fatalf("Error getting key '%s' from cache: %v", key, err)
		return
	}
	log.Printf(res.GetData())
}

func (c *ClientWrapper) PutGrpc(key string, value string) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	_, err := nodeInfo.GrpcClient.Put(ctx, &pb.PutRequest{Key: key, Value: value})
	if err != nil {
		log.Fatalf("Error putting key '%s' value '%s' into cache: %v", key, value, err)
		return
	}
}


// Utility function to set up mTLS config and credentials
func LoadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile("../../certs/ca-cert.pem")
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair("../../certs/client-cert.pem", "../../certs/client-key.pem")
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

// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func NewCacheClient(server_host string, server_port int) pb.CacheServiceClient {
	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		log.Fatalf("failed to create credentials: %v", err)
	}

	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping back
		PermitWithoutStream: true,             // send pings even without active streams
	}

	// set up connection
	addr := fmt.Sprintf("%s:%d", server_host, server_port)
	conn, err := grpc.Dial(
		addr,	
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(kacp),
	)
	if err != nil {
		panic(err)
	}	

	// set up client
	return pb.NewCacheServiceClient(conn)
}